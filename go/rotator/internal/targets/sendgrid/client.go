package sendgrid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const apiBase = "https://api.sendgrid.com"

type Client struct {
	adminKey   string
	httpClient *http.Client
}

type KeyResponse struct {
	ID  string `json:"api_key_id"`
	Key string `json:"api_key"`
}

func NewClient(adminKey string) *Client {
	return &Client{adminKey: adminKey, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) CreateKey(ctx context.Context, name string, scopes []string) (*KeyResponse, error) {
	if len(scopes) == 0 {
		scopes = []string{"mail.send"}
	}
	body, _ := json.Marshal(map[string]interface{}{"name": name, "scopes": scopes})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/v3/api_keys", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.adminKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("create key (HTTP %d): %s", resp.StatusCode, string(respBody))
	}
	var key KeyResponse
	if err := json.Unmarshal(respBody, &key); err != nil {
		return nil, err
	}
	return &key, nil
}

func (c *Client) DeleteKey(ctx context.Context, keyID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, apiBase+"/v3/api_keys/"+keyID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.adminKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete key (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}
