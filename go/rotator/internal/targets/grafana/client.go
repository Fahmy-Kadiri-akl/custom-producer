package grafana

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
	ID  int    `json:"id"`
	Key string `json:"key"`
}

func NewClient(baseURL, adminToken string) *Client {
	return &Client{baseURL: baseURL, adminToken: adminToken, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) CreateToken(ctx context.Context, saID int, name string, expiry int) (*TokenResponse, error) {
	payload := map[string]interface{}{"name": name, "secondsToLive": expiry}
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/api/serviceaccounts/%d/tokens", c.baseURL, saID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.adminToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("create token (HTTP %d): %s", resp.StatusCode, string(respBody))
	}
	var token TokenResponse
	if err := json.Unmarshal(respBody, &token); err != nil {
		return nil, err
	}
	return &token, nil
}

func (c *Client) DeleteToken(ctx context.Context, saID, tokenID int) error {
	url := fmt.Sprintf("%s/api/serviceaccounts/%d/tokens/%d", c.baseURL, saID, tokenID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.adminToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete token (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}
