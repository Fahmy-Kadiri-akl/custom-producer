package okta

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
	orgURL     string
	adminToken string
	httpClient *http.Client
}

type TokenResponse struct {
	ID        string `json:"id"`
	TokenName string `json:"name"`
	Token     string `json:"tokenWindow"` // Only returned on create
}

func NewClient(orgURL, adminToken string) *Client {
	return &Client{orgURL: orgURL, adminToken: adminToken, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) do(ctx context.Context, method, path string, body interface{}) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		bs, _ := json.Marshal(body)
		reqBody = bytes.NewReader(bs)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.orgURL+path, reqBody)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "SSWS "+c.adminToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, nil
}

// CreateToken creates a new API token. Note: Okta API tokens inherit the
// permissions of the user whose SSWS token is used to create them.
func (c *Client) CreateToken(ctx context.Context, name string) (*TokenResponse, error) {
	body, status, err := c.do(ctx, http.MethodPost, "/api/v1/api-tokens", map[string]string{"name": name})
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return nil, fmt.Errorf("create token (HTTP %d): %s", status, string(body))
	}
	var token TokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, err
	}
	return &token, nil
}

// RevokeToken deactivates and deletes an API token.
func (c *Client) RevokeToken(ctx context.Context, tokenID string) error {
	body, status, err := c.do(ctx, http.MethodDelete, "/api/v1/api-tokens/"+tokenID, nil)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("revoke token (HTTP %d): %s", status, string(body))
	}
	return nil
}
