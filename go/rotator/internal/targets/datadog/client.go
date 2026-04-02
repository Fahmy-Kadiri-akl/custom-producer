package datadog

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
	site       string
	apiKey     string
	appKey     string
	httpClient *http.Client
}

type KeyResponse struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

func NewClient(site, apiKey, appKey string) *Client {
	if site == "" {
		site = "datadoghq.com"
	}
	return &Client{site: site, apiKey: apiKey, appKey: appKey, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) base() string { return fmt.Sprintf("https://api.%s", c.site) }

func (c *Client) do(ctx context.Context, method, path string, body interface{}) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		bs, _ := json.Marshal(body)
		reqBody = bytes.NewReader(bs)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base()+path, reqBody)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("DD-API-KEY", c.apiKey)
	req.Header.Set("DD-APPLICATION-KEY", c.appKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, nil
}

func (c *Client) CreateAPIKey(ctx context.Context, name string) (*KeyResponse, error) {
	body, status, err := c.do(ctx, http.MethodPost, "/api/v2/api_keys", map[string]interface{}{
		"data": map[string]interface{}{"type": "api_keys", "attributes": map[string]string{"name": name}},
	})
	if err != nil {
		return nil, err
	}
	if status != http.StatusCreated {
		return nil, fmt.Errorf("create API key (HTTP %d): %s", status, string(body))
	}
	var resp struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				Key string `json:"key"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &KeyResponse{ID: resp.Data.ID, Key: resp.Data.Attributes.Key}, nil
}

func (c *Client) DeleteAPIKey(ctx context.Context, keyID string) error {
	body, status, err := c.do(ctx, http.MethodDelete, "/api/v2/api_keys/"+keyID, nil)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("delete API key (HTTP %d): %s", status, string(body))
	}
	return nil
}

func (c *Client) CreateAppKey(ctx context.Context, name string) (*KeyResponse, error) {
	body, status, err := c.do(ctx, http.MethodPost, "/api/v2/application_keys", map[string]interface{}{
		"data": map[string]interface{}{"type": "application_keys", "attributes": map[string]string{"name": name}},
	})
	if err != nil {
		return nil, err
	}
	if status != http.StatusCreated {
		return nil, fmt.Errorf("create app key (HTTP %d): %s", status, string(body))
	}
	var resp struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				Key string `json:"key"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &KeyResponse{ID: resp.Data.ID, Key: resp.Data.Attributes.Key}, nil
}

func (c *Client) DeleteAppKey(ctx context.Context, keyID string) error {
	body, status, err := c.do(ctx, http.MethodDelete, "/api/v2/application_keys/"+keyID, nil)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("delete app key (HTTP %d): %s", status, string(body))
	}
	return nil
}
