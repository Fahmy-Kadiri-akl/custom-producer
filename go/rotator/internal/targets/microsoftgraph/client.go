// Package microsoftgraph provides a rotation target for Entra ID (Azure AD)
// application client secrets, using the Microsoft Graph application
// addPassword / removePassword API. The rotator authenticates as a dedicated
// app registration with a certificate, not as the target application.
package microsoftgraph

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	graphTokenURL       = "https://login.microsoftonline.com/%s/oauth2/v2.0/token"
	graphBase           = "https://graph.microsoft.com/v1.0"
	graphScope          = "https://graph.microsoft.com/.default"
	clientAssertionType = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"
	assertionTTL        = 5 * time.Minute
	httpTimeout         = 30 * time.Second
)

// Client authenticates as the rotator app registration with a certificate
// (client_credentials + RFC 7519 client_assertion) and calls the Graph
// application addPassword / removePassword API on target apps.
type Client struct {
	tenantID string
	clientID string
	cert     *x509.Certificate
	key      *rsa.PrivateKey
	http     *http.Client

	// Per-instance access-token cache so a single Rotate call, which issues
	// addPassword then removePassword, exchanges credentials only once.
	cachedToken  string
	cachedExpiry time.Time
}

// NewClient parses the rotator's certificate and RSA private key from PEM.
func NewClient(tenantID, clientID, certPEM, keyPEM string) (*Client, error) {
	certBlock, _ := pem.Decode([]byte(certPEM))
	if certBlock == nil {
		return nil, fmt.Errorf("decode rotator certificate PEM: no PEM block found")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse rotator certificate: %w", err)
	}

	keyBlock, _ := pem.Decode([]byte(keyPEM))
	if keyBlock == nil {
		return nil, fmt.Errorf("decode rotator key PEM: no PEM block found")
	}
	var keyAny interface{}
	if k, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes); err == nil {
		keyAny = k
	} else if k, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes); err == nil {
		keyAny = k
	} else {
		return nil, fmt.Errorf("parse rotator private key: unsupported PEM (tried PKCS8 and PKCS1)")
	}
	rsaKey, ok := keyAny.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("rotator private key is not RSA; RS256 assertions require an RSA key")
	}

	return &Client{
		tenantID: tenantID,
		clientID: clientID,
		cert:     cert,
		key:      rsaKey,
		http:     &http.Client{Timeout: httpTimeout},
	}, nil
}

// clientAssertion builds a short-lived RS256 JWT signed with the rotator's
// private key, used as the client_assertion in the client_credentials exchange.
// The header carries x5t (SHA-1 thumbprint) and x5c (the full certificate):
// Entra rejects the assertion without x5t/x5t#256/kid (AADSTS700027), using it
// to look up the registered key credential, while x5c lets it validate the
// signature.
func (c *Client) clientAssertion(now time.Time) (string, error) {
	fp := sha1.Sum(c.cert.Raw)
	header := map[string]interface{}{
		"alg": "RS256",
		"typ": "JWT",
		"x5t": base64.RawURLEncoding.EncodeToString(fp[:]),
		"x5c": []string{base64.StdEncoding.EncodeToString(c.cert.Raw)},
	}
	jti, err := randomID()
	if err != nil {
		return "", fmt.Errorf("generate assertion jti: %w", err)
	}
	claims := map[string]interface{}{
		"aud": fmt.Sprintf(graphTokenURL, c.tenantID),
		"iss": c.clientID,
		"sub": c.clientID,
		"iat": now.Unix(),
		"nbf": now.Unix(),
		"exp": now.Add(assertionTTL).Unix(),
		"jti": jti,
	}
	hb, _ := json.Marshal(header)
	cb, _ := json.Marshal(claims)
	signedInput := base64.RawURLEncoding.EncodeToString(hb) + "." + base64.RawURLEncoding.EncodeToString(cb)
	digest := sha256.Sum256([]byte(signedInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, c.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign client assertion: %w", err)
	}
	return signedInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// accessToken exchanges the rotator's certificate for a Graph access token.
// The result is cached until 60s before expiry so one Rotate call reuses it.
func (c *Client) accessToken(ctx context.Context) (string, error) {
	now := time.Now().UTC()
	if c.cachedToken != "" && now.Before(c.cachedExpiry) {
		return c.cachedToken, nil
	}

	assertion, err := c.clientAssertion(now)
	if err != nil {
		return "", err
	}
	form := url.Values{
		"grant_type":            {"client_credentials"},
		"client_id":             {c.clientID},
		"client_assertion_type": {clientAssertionType},
		"client_assertion":      {assertion},
		"scope":                 {graphScope},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf(graphTokenURL, c.tenantID), bytes.NewBufferString(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("client_credentials exchange (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("empty access_token in response: %s", string(body))
	}
	if tr.ExpiresIn > 60 {
		c.cachedToken = tr.AccessToken
		c.cachedExpiry = now.Add(time.Duration(tr.ExpiresIn-60) * time.Second)
	}
	return tr.AccessToken, nil
}

// addedPassword is the passwordCredential object Graph returns from addPassword.
type addedPassword struct {
	SecretText  string `json:"secretText"`
	KeyID       string `json:"keyId"`
	EndDateTime string `json:"endDateTime,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

// addPassword creates a new client secret on the target application identified
// by its appId, returning the secret value and its keyId. validDays sets the
// secret's lifetime and should outlast the rotation interval.
func (c *Client) addPassword(ctx context.Context, clientID, displayName string, validDays int) (*addedPassword, error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	if displayName == "" {
		displayName = "akeyless-rotated"
	}
	now := time.Now().UTC()
	end := now.Add(time.Duration(validDays) * 24 * time.Hour)
	body, _ := json.Marshal(map[string]interface{}{
		"passwordCredential": map[string]interface{}{
			"displayName":   displayName,
			"startDateTime": now.Format(time.RFC3339),
			"endDateTime":   end.Format(time.RFC3339),
		},
	})
	apiURL := fmt.Sprintf("%s/applications(appId='%s')/addPassword", graphBase, clientID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build addPassword request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("addPassword request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("addPassword (HTTP %d): %s", resp.StatusCode, string(respBody))
	}
	var added addedPassword
	if err := json.Unmarshal(respBody, &added); err != nil {
		return nil, fmt.Errorf("parse addPassword response: %w", err)
	}
	if added.SecretText == "" || added.KeyID == "" {
		return nil, fmt.Errorf("addPassword returned no secretText/keyId: %s", string(respBody))
	}
	return &added, nil
}

// removePassword deletes a client secret by keyId from the target application.
// Entra returns HTTP 409 Directory_ConcurrencyViolation when this runs right
// after addPassword on the same app, because the object change has not yet
// converged across replicas; its own guidance is to wait briefly and retry. So
// removePassword retries transient 409/429/5xx responses with backoff.
func (c *Client) removePassword(ctx context.Context, clientID, keyID string) error {
	const maxAttempts = 4
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := c.removePasswordOnce(ctx, clientID, keyID)
		if err == nil {
			return nil
		}
		lastErr = err
		var he *httpError
		if !errors.As(err, &he) || !he.retryable() {
			return err
		}
		if attempt < maxAttempts {
			select {
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return lastErr
}

func (c *Client) removePasswordOnce(ctx context.Context, clientID, keyID string) error {
	token, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{"keyId": keyID})
	apiURL := fmt.Sprintf("%s/applications(appId='%s')/removePassword", graphBase, clientID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build removePassword request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("removePassword request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(resp.Body)
		return &httpError{status: resp.StatusCode, body: string(rb)}
	}
	return nil
}

// httpError carries a non-2xx Graph response so the caller can decide to retry.
type httpError struct {
	status int
	body   string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("removePassword (HTTP %d): %s", e.status, e.body)
}

// retryable reports whether the Graph response is a transient condition worth
// retrying: 409 concurrency, 429 throttling, or any 5xx.
func (e *httpError) retryable() bool {
	switch e.status {
	case http.StatusConflict, http.StatusTooManyRequests:
		return true
	default:
		return e.status >= 500
	}
}

// randomID returns 16 bytes of crypto/rand as an unpadded base64url string,
// used for the assertion jti claim.
func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
