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
// rotate endpoint.
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

// RotateToken mints a fresh token for the service account and invalidates the
// previous one in a single atomic call. The first_name/last_name body fields
// are required by the API and preserve the account display name.
func (c *Client) RotateToken(ctx context.Context, org, email, firstName, lastName string) (*TokenResponse, error) {
	body, _ := json.Marshal(map[string]string{
		"first_name": firstName,
		"last_name":  lastName,
	})
	url := fmt.Sprintf("%s/api/%s/service_accounts/%s?rotateToken=true", c.baseURL, org, email)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.adminUsername, c.adminPassword)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rotate token (HTTP %d): %s", resp.StatusCode, string(respBody))
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
