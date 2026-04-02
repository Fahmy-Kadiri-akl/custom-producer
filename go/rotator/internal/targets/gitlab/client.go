package gitlab

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
	baseURL    string
	adminToken string
	httpClient *http.Client
}

type TokenResponse struct {
	ID    int    `json:"id"`
	Token string `json:"token"`
}

func NewClient(baseURL, adminToken string) *Client {
	return &Client{baseURL: baseURL, adminToken: adminToken, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) CreatePAT(ctx context.Context, userID int, name string, scopes []string, expiryDays int) (*TokenResponse, error) {
	expiresAt := time.Now().UTC().Add(time.Duration(expiryDays) * 24 * time.Hour).Format("2006-01-02")
	body, _ := json.Marshal(map[string]interface{}{
		"name":       name,
		"scopes":     scopes,
		"expires_at": expiresAt,
	})
	url := fmt.Sprintf("%s/api/v4/users/%d/personal_access_tokens", c.baseURL, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("PRIVATE-TOKEN", c.adminToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("create PAT (HTTP %d): %s", resp.StatusCode, string(respBody))
	}
	var token TokenResponse
	if err := json.Unmarshal(respBody, &token); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &token, nil
}

func (c *Client) RevokePAT(ctx context.Context, tokenID int) error {
	url := fmt.Sprintf("%s/api/v4/personal_access_tokens/%d", c.baseURL, tokenID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("PRIVATE-TOKEN", c.adminToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("revoke PAT (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}
