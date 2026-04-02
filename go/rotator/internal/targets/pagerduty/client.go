package pagerduty

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const apiBase = "https://api.pagerduty.com"

type Client struct {
	adminToken string
	httpClient *http.Client
}

type KeyResponse struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

func NewClient(adminToken string) *Client {
	return &Client{adminToken: adminToken, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) CreateKey(ctx context.Context, name, keyType string) (*KeyResponse, error) {
	readOnly := keyType == "read_only"
	payload, _ := json.Marshal(map[string]interface{}{
		"key": map[string]interface{}{
			"description": name,
			"type":        "sso_only_full_access_rest_api_key",
			"read_only":   readOnly,
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/api_keys", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Token token="+c.adminToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("create key (HTTP %d): %s", resp.StatusCode, string(body))
	}
	var result struct {
		Key struct {
			ID  string `json:"id"`
			Key string `json:"key"`
		} `json:"key"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &KeyResponse{ID: result.Key.ID, Key: result.Key.Key}, nil
}

func (c *Client) DeleteKey(ctx context.Context, keyID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, apiBase+"/api_keys/"+keyID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token token="+c.adminToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete key (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}
