package openobserve

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL       string
	adminUsername string
	adminPassword string
	httpClient    *http.Client
}

// TokenResponse is the body returned by the OpenObserve service-account
// token and rotate endpoints.
type TokenResponse struct {
	Token string `json:"token"`
	User  string `json:"user"`
}

func NewClient(baseURL, adminUsername, adminPassword string) *Client {
	return &Client{
		baseURL:       baseURL,
		adminUsername: adminUsername,
		adminPassword: adminPassword,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, method, url string, body []byte) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.SetBasicAuth(c.adminUsername, c.adminPassword)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, respBody, nil
}

// CreateServiceAccount provisions a new service account. The token is issued
// at creation but not returned in this response; fetch it with GetToken.
func (c *Client) CreateServiceAccount(ctx context.Context, org, email, firstName, lastName string) error {
	body, _ := json.Marshal(map[string]string{
		"email":        email,
		"organization": org,
		"first_name":   firstName,
		"last_name":    lastName,
	})
	url := fmt.Sprintf("%s/api/%s/service_accounts", c.baseURL, org)
	code, respBody, err := c.do(ctx, http.MethodPost, url, body)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("create service account (HTTP %d): %s", code, string(respBody))
	}
	return nil
}

// GetToken returns the current token for a service account.
func (c *Client) GetToken(ctx context.Context, org, email string) (string, error) {
	url := fmt.Sprintf("%s/api/%s/service_accounts/%s", c.baseURL, org, email)
	code, respBody, err := c.do(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	if code != http.StatusOK {
		return "", fmt.Errorf("get token (HTTP %d): %s", code, string(respBody))
	}
	var tr TokenResponse
	if err := json.Unmarshal(respBody, &tr); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if tr.Token == "" {
		return "", fmt.Errorf("empty token in response: %s", string(respBody))
	}
	return tr.Token, nil
}

// DeleteServiceAccount removes a service account, invalidating its token. A
// missing account is treated as already deleted (idempotent).
func (c *Client) DeleteServiceAccount(ctx context.Context, org, email string) error {
	url := fmt.Sprintf("%s/api/%s/service_accounts/%s", c.baseURL, org, email)
	code, respBody, err := c.do(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	if code != http.StatusOK && code != http.StatusNotFound {
		return fmt.Errorf("delete service account (HTTP %d): %s", code, string(respBody))
	}
	return nil
}

// RotateToken mints a fresh token for an existing service account and
// invalidates the previous one in a single atomic call. first_name/last_name
// are required by the API and preserve the account display name.
func (c *Client) RotateToken(ctx context.Context, org, email, firstName, lastName string) (*TokenResponse, error) {
	body, _ := json.Marshal(map[string]string{
		"first_name": firstName,
		"last_name":  lastName,
	})
	url := fmt.Sprintf("%s/api/%s/service_accounts/%s?rotateToken=true", c.baseURL, org, email)
	code, respBody, err := c.do(ctx, http.MethodPut, url, body)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("rotate token (HTTP %d): %s", code, string(respBody))
	}
	var token TokenResponse
	if err := json.Unmarshal(respBody, &token); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if token.Token == "" {
		return nil, fmt.Errorf("empty token in response: %s", string(respBody))
	}
	return &token, nil
}
