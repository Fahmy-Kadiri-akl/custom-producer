package argocd

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

type Client struct {
	baseURL    string
	adminToken string
	httpClient *http.Client
}

type TokenResponse struct {
	Token    string `json:"token"`
	IssuedAt string `json:"iat,omitempty"`
}

func NewClient(baseURL, adminToken string) *Client {
	return &Client{
		baseURL: baseURL, adminToken: adminToken,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		},
	}
}

func (c *Client) CreateToken(ctx context.Context, account string, expiresIn int) (*TokenResponse, error) {
	payload, _ := json.Marshal(map[string]interface{}{
		"expiresIn": expiresIn,
	})
	url := fmt.Sprintf("%s/api/v1/account/%s/token", c.baseURL, account)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", "argocd.token="+c.adminToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("create token (HTTP %d): %s", resp.StatusCode, string(body))
	}
	var token TokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, err
	}
	return &token, nil
}

func (c *Client) DeleteToken(ctx context.Context, account, tokenID string) error {
	url := fmt.Sprintf("%s/api/v1/account/%s/token/%s", c.baseURL, account, tokenID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Cookie", "argocd.token="+c.adminToken)
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
