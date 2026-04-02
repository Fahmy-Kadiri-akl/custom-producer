// Package ansible provides an HTTP client for the Ansible Automation Platform
// (AAP/AWX) API and rotation targets for user passwords and API tokens.
package ansible

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client communicates with the Ansible AAP/AWX API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new Ansible API client.
func NewClient(baseURL string, skipTLSVerify bool) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: skipTLSVerify}},
		},
	}
}

func (c *Client) do(ctx context.Context, method, path string, username, password string, body interface{}) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		bs, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewReader(bs)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if username != "" {
		req.SetBasicAuth(username, password)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}
	return respBody, resp.StatusCode, nil
}

// UpdateUserPassword changes the password for an Ansible user.
func (c *Client) UpdateUserPassword(ctx context.Context, adminUser, adminPass string, userID int, newPassword string) error {
	path := fmt.Sprintf("/api/v2/users/%d/", userID)
	body := map[string]string{"password": newPassword}
	respBody, status, err := c.do(ctx, http.MethodPatch, path, adminUser, adminPass, body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("update password: HTTP %d: %s", status, string(respBody))
	}
	return nil
}

// LookupUserByUsername finds a user by username and returns their ID.
func (c *Client) LookupUserByUsername(ctx context.Context, adminUser, adminPass, username string) (int, error) {
	path := fmt.Sprintf("/api/v2/users/?username=%s", username)
	respBody, status, err := c.do(ctx, http.MethodGet, path, adminUser, adminPass, nil)
	if err != nil {
		return 0, err
	}
	if status != http.StatusOK {
		return 0, fmt.Errorf("lookup user: HTTP %d: %s", status, string(respBody))
	}
	var result struct {
		Count   int `json:"count"`
		Results []struct {
			ID int `json:"id"`
		} `json:"results"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return 0, fmt.Errorf("parse lookup: %w", err)
	}
	if result.Count == 0 {
		return 0, fmt.Errorf("user %q not found", username)
	}
	return result.Results[0].ID, nil
}

// TokenResponse represents an Ansible personal access token.
type TokenResponse struct {
	ID    int    `json:"id"`
	Token string `json:"token"`
}

// CreatePersonalToken creates a new personal access token for the given user.
func (c *Client) CreatePersonalToken(ctx context.Context, adminUser, adminPass string, userID int, description, scope string) (*TokenResponse, error) {
	path := fmt.Sprintf("/api/v2/users/%d/personal_tokens/", userID)
	body := map[string]string{"description": description, "scope": scope}
	respBody, status, err := c.do(ctx, http.MethodPost, path, adminUser, adminPass, body)
	if err != nil {
		return nil, err
	}
	if status != http.StatusCreated {
		return nil, fmt.Errorf("create token: HTTP %d: %s", status, string(respBody))
	}
	var token TokenResponse
	if err := json.Unmarshal(respBody, &token); err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	return &token, nil
}

// RevokeToken deletes a personal access token by ID.
func (c *Client) RevokeToken(ctx context.Context, adminUser, adminPass string, tokenID int) error {
	path := fmt.Sprintf("/api/v2/tokens/%d/", tokenID)
	_, status, err := c.do(ctx, http.MethodDelete, path, adminUser, adminPass, nil)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("revoke token: unexpected HTTP %d", status)
	}
	return nil
}
