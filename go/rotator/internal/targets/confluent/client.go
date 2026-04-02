package confluent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const apiBase = "https://api.confluent.cloud"

type Client struct {
	apiKey     string
	apiSecret  string
	httpClient *http.Client
}

type KeyResponse struct {
	ID     string `json:"id"`
	Secret string `json:"secret"`
}

func NewClient(apiKey, apiSecret string) *Client {
	return &Client{apiKey: apiKey, apiSecret: apiSecret, httpClient: &http.Client{Timeout: 30 * time.Second}}
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
	req.SetBasicAuth(c.apiKey, c.apiSecret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, nil
}

func (c *Client) CreateAPIKey(ctx context.Context, ownerID, resourceID, description string) (*KeyResponse, error) {
	payload := map[string]interface{}{
		"spec": map[string]interface{}{
			"display_name": description,
			"description":  description,
			"owner":        map[string]string{"id": ownerID},
			"resource":     map[string]string{"id": resourceID},
		},
	}
	body, status, err := c.do(ctx, http.MethodPost, "/iam/v2/api-keys", payload)
	if err != nil {
		return nil, err
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return nil, fmt.Errorf("create API key (HTTP %d): %s", status, string(body))
	}
	var resp struct {
		ID   string `json:"id"`
		Spec struct {
			Secret string `json:"secret"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &KeyResponse{ID: resp.ID, Secret: resp.Spec.Secret}, nil
}

func (c *Client) DeleteAPIKey(ctx context.Context, keyID string) error {
	body, status, err := c.do(ctx, http.MethodDelete, "/iam/v2/api-keys/"+keyID, nil)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("delete API key (HTTP %d): %s", status, string(body))
	}
	return nil
}
