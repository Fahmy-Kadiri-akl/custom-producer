package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const apiBase = "https://api.cloudflare.com/client/v4"

type Client struct {
	adminToken string
	httpClient *http.Client
}

type TokenResponse struct {
	ID    string `json:"id"`
	Value string `json:"value"` // only returned on create
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
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, nil
}

func (c *Client) CreateToken(ctx context.Context, name string, policies []map[string]interface{}, condition map[string]interface{}) (*TokenResponse, error) {
	payload := map[string]interface{}{
		"name":     name,
		"policies": policies,
	}
	if condition != nil {
		payload["condition"] = condition
	}
	body, status, err := c.do(ctx, http.MethodPost, "/user/tokens", payload)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return nil, fmt.Errorf("create token (HTTP %d): %s", status, string(body))
	}
	var resp struct {
		Result TokenResponse `json:"result"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &resp.Result, nil
}

func (c *Client) DeleteToken(ctx context.Context, tokenID string) error {
	body, status, err := c.do(ctx, http.MethodDelete, "/user/tokens/"+tokenID, nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("delete token (HTTP %d): %s", status, string(body))
	}
	return nil
}
