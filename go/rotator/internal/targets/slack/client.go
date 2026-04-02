package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const tokenURL = "https://slack.com/api/tooling.tokens.rotate"

type Client struct {
	httpClient *http.Client
}

type RotateResponse struct {
	OK           bool   `json:"ok"`
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error,omitempty"`
}

func NewClient() *Client {
	return &Client{httpClient: &http.Client{Timeout: 30 * time.Second}}
}

// RotateToken uses Slack's token rotation API to get a new token pair.
func (c *Client) RotateToken(ctx context.Context, refreshToken, clientID, clientSecret string) (*RotateResponse, error) {
	data := url.Values{
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"grant_type":    {"refresh_token"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, nil)
	if err != nil {
		return nil, err
	}
	req.URL.RawQuery = data.Encode()
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result RotateResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if !result.OK {
		return nil, fmt.Errorf("slack error: %s", result.Error)
	}
	return &result, nil
}
