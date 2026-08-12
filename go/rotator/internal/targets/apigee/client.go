package apigee

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	googleTokenURL  = "https://oauth2.googleapis.com/token"
	defaultScope    = "https://www.googleapis.com/auth/cloud-platform"
	assertionTTL    = time.Hour
	defaultSAEnvVar = "APIGEE_SERVICE_ACCOUNT_JSON"
)

// saKey holds the fields of a GCP service-account key file that are needed
// to sign a JWT-bearer assertion.
type saKey struct {
	PrivateKeyID string `json:"private_key_id"`
	PrivateKey   string `json:"private_key"`
	ClientEmail  string `json:"client_email"`
	TokenURI     string `json:"token_uri"`
}

// effectiveScope returns the requested scope, falling back to the Apigee
// management-API default when the payload leaves it blank. This is the single
// place the default is resolved.
func effectiveScope(s string) string {
	if s == "" {
		return defaultScope
	}
	return s
}

// resolveServiceAccount reads the GCP service-account key JSON from the
// rotator environment. ref names the environment variable holding the key and
// defaults to APIGEE_SERVICE_ACCOUNT_JSON when empty. Holding the key in the
// deployment environment instead of the round-tripped payload keeps the
// long-lived private key out of the value that secret consumers read.
func resolveServiceAccount(ref string) (string, error) {
	name := ref
	if name == "" {
		name = defaultSAEnvVar
	}
	val := os.Getenv(name)
	if val == "" {
		return "", fmt.Errorf("service account key not found: environment variable %q is not set", name)
	}
	return val, nil
}

// mintAccessToken exchanges a service-account key for an OAuth2 access token
// using the JWT-bearer grant (RFC 7523). It returns the access token and its
// lifetime in seconds. The scope must already be resolved by the caller via
// effectiveScope.
func mintAccessToken(ctx context.Context, saJSON, scope string) (string, int, error) {
	var key saKey
	if err := json.Unmarshal([]byte(saJSON), &key); err != nil {
		return "", 0, fmt.Errorf("parse service_account_json: %w", err)
	}
	if key.PrivateKey == "" || key.ClientEmail == "" {
		return "", 0, fmt.Errorf("service_account_json is missing private_key or client_email")
	}
	tokenURL := key.TokenURI
	if tokenURL == "" {
		tokenURL = googleTokenURL
	}

	assertion, err := buildAssertion(key, scope, tokenURL)
	if err != nil {
		return "", 0, fmt.Errorf("build JWT assertion: %w", err)
	}

	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", assertion)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", 0, fmt.Errorf("read token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("jwt-bearer exchange (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", 0, fmt.Errorf("parse token response: %w", err)
	}
	if tr.AccessToken == "" {
		return "", 0, fmt.Errorf("empty access_token in response: %s", string(body))
	}
	if tr.ExpiresIn <= 0 {
		tr.ExpiresIn = 3600
	}
	return tr.AccessToken, tr.ExpiresIn, nil
}

// buildAssertion constructs and signs the RS256 JWT used as the bearer
// assertion. The JWT claims issuer, scope, audience, issued-at, expiry, and a
// unique jti so repeated assertions differ.
func buildAssertion(key saKey, scope, aud string) (string, error) {
	header := map[string]string{
		"alg": "RS256",
		"typ": "JWT",
	}
	if key.PrivateKeyID != "" {
		header["kid"] = key.PrivateKeyID
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("marshal header: %w", err)
	}

	now := time.Now().UTC()
	var jti [16]byte
	if _, err := rand.Read(jti[:]); err != nil {
		return "", fmt.Errorf("generate jti: %w", err)
	}
	claims := map[string]any{
		"iss":   key.ClientEmail,
		"scope": scope,
		"aud":   aud,
		"iat":   now.Unix(),
		"exp":   now.Add(assertionTTL).Unix(),
		"jti":   base64.RawURLEncoding.EncodeToString(jti[:]),
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal claims: %w", err)
	}

	enc := base64.RawURLEncoding
	signingInput := enc.EncodeToString(headerJSON) + "." + enc.EncodeToString(claimsJSON)

	privKey, err := parseRSAPrivateKey(key.PrivateKey)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign assertion: %w", err)
	}
	return signingInput + "." + enc.EncodeToString(signature), nil
}

// parseRSAPrivateKey parses the PEM-encoded RSA private key carried by a
// service-account key file. GCP keys are PKCS#8; PKCS#1 is accepted as a
// fallback for older key material.
func parseRSAPrivateKey(pemData string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, fmt.Errorf("no PEM block in private_key")
	}
	if parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rsaKey, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key is not RSA")
		}
		return rsaKey, nil
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, fmt.Errorf("parse private key as PKCS#8 or PKCS#1 failed")
}
