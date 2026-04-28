package github

import (
	"bytes"
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
	"time"
)

const apiBase = "https://api.github.com"

// InstallationToken is the response from POST /app/installations/{id}/access_tokens.
type InstallationToken struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

// MintInstallationToken mints a fresh installation access token by signing a
// short-lived JWT with the App's private key, then exchanging it via the
// /app/installations/{id}/access_tokens endpoint.
//
// repos/repoIDs/perms are forwarded to GitHub if non-empty to scope the token.
func MintInstallationToken(
	ctx context.Context,
	appID, installationID int64,
	privateKeyPEM string,
	repos []string,
	repoIDs []int64,
	perms map[string]string,
) (*InstallationToken, error) {
	jwt, err := signAppJWT(appID, privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("sign app JWT: %w", err)
	}

	body := map[string]interface{}{}
	if len(repos) > 0 {
		body["repositories"] = repos
	}
	if len(repoIDs) > 0 {
		body["repository_ids"] = repoIDs
	}
	if len(perms) > 0 {
		body["permissions"] = perms
	}
	var reqBody io.Reader
	if len(body) > 0 {
		bs, _ := json.Marshal(body)
		reqBody = bytes.NewReader(bs)
	}

	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", apiBase, installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mint installation token (HTTP %d): %s", resp.StatusCode, string(respBody))
	}
	var tok InstallationToken
	if err := json.Unmarshal(respBody, &tok); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if tok.Token == "" {
		return nil, fmt.Errorf("empty token in response: %s", string(respBody))
	}
	return &tok, nil
}

// RevokeInstallationToken invalidates a still-active installation access
// token before its natural expiry. Best-effort; GitHub auto-expires tokens
// after ~1h regardless.
func RevokeInstallationToken(ctx context.Context, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, apiBase+"/installation/token", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("revoke installation token (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// signAppJWT builds an RS256-signed JWT suitable for authenticating as a
// GitHub App. iat is set 30s in the past to tolerate small clock skew; exp
// is 9 minutes in the future (GitHub's max is 10).
func signAppJWT(appID int64, privateKeyPEM string) (string, error) {
	key, err := parseRSAPrivateKey(privateKeyPEM)
	if err != nil {
		return "", err
	}
	now := time.Now()
	headerJSON, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	claimsJSON, _ := json.Marshal(map[string]interface{}{
		"iat": now.Add(-30 * time.Second).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": appID,
	})
	enc := base64.RawURLEncoding
	signingInput := enc.EncodeToString(headerJSON) + "." + enc.EncodeToString(claimsJSON)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("rsa sign: %w", err)
	}
	return signingInput + "." + enc.EncodeToString(signature), nil
}

func parseRSAPrivateKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("private_key is not valid PEM")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key (tried PKCS#1 and PKCS#8): %w", err)
	}
	rsaKey, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not RSA")
	}
	return rsaKey, nil
}
