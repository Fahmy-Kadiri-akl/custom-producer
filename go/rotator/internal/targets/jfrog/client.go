package jfrog

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
	TokenID     string `json:"token_id"`
	AccessToken string `json:"access_token"`
	Scope       string `json:"scope"`
	ExpiresIn   int    `json:"expires_in"`
}

func NewClient(baseURL, adminToken string) *Client {
	return &Client{baseURL: baseURL, adminToken: adminToken, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) CreateToken(ctx context.Context, username, scope, description string, expiresIn int) (*TokenResponse, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"subject":     username,
		"scope":       scope,
		"description": description,
		"expires_in":  expiresIn,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/access/api/v1/tokens", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.adminToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("create token (HTTP %d): %s", resp.StatusCode, string(respBody))
	}
	var token TokenResponse
	if err := json.Unmarshal(respBody, &token); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &token, nil
}

func (c *Client) RevokeToken(ctx context.Context, tokenID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/access/api/v1/tokens/"+tokenID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.adminToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("revoke token (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}
