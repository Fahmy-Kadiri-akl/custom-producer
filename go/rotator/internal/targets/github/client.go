package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const apiBase = "https://api.github.com"

type Client struct {
	adminToken string
	httpClient *http.Client
}

type TokenResponse struct {
	ID    int    `json:"id"`
	Token string `json:"token"`
}

func NewClient(adminToken string) *Client {
	return &Client{adminToken: adminToken, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) do(ctx context.Context, method, path string, body interface{}) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		bs, _ := json.Marshal(body)
		reqBody = bytes.NewReader(bs)
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, reqBody)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.adminToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, nil
}

// CreateFineGrainedPAT creates a new fine-grained PAT via the org API.
func (c *Client) CreateFineGrainedPAT(ctx context.Context, owner, tokenName string, repos []string, permissions map[string]string, expiryDays int) (*TokenResponse, error) {
	expiry := time.Now().UTC().Add(time.Duration(expiryDays) * 24 * time.Hour).Format(time.RFC3339)
	payload := map[string]interface{}{
		"name":         tokenName,
		"repositories": repos,
		"permissions":  permissions,
		"expires_at":   expiry,
	}
	body, status, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/orgs/%s/personal-access-tokens", owner), payload)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return nil, fmt.Errorf("create PAT (HTTP %d): %s", status, string(body))
	}
	var token TokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &token, nil
}

// RevokeFineGrainedPAT revokes a fine-grained PAT by ID.
func (c *Client) RevokeFineGrainedPAT(ctx context.Context, owner string, tokenID int) error {
	body, status, err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/orgs/%s/personal-access-tokens/%d", owner, tokenID), nil)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK && status != http.StatusAccepted {
		return fmt.Errorf("revoke PAT (HTTP %d): %s", status, string(body))
	}
	return nil
}
