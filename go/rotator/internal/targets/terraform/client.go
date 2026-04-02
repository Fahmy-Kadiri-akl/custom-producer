package terraform

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
	ID    string `json:"id"`
	Token string `json:"token"`
}

func NewClient(baseURL, adminToken string) *Client {
	if baseURL == "" {
		baseURL = "https://app.terraform.io"
	}
	return &Client{baseURL: baseURL, adminToken: adminToken, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) do(ctx context.Context, method, path string, body interface{}) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		bs, _ := json.Marshal(body)
		reqBody = bytes.NewReader(bs)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.adminToken)
	req.Header.Set("Content-Type", "application/vnd.api+json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, nil
}

func (c *Client) CreateTeamToken(ctx context.Context, teamID string) (*TokenResponse, error) {
	body, status, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v2/teams/%s/authentication-token", teamID), nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return nil, fmt.Errorf("create team token (HTTP %d): %s", status, string(body))
	}
	return parseTokenResponse(body)
}

func (c *Client) CreateOrgToken(ctx context.Context, orgName string) (*TokenResponse, error) {
	body, status, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v2/organizations/%s/authentication-token", orgName), nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return nil, fmt.Errorf("create org token (HTTP %d): %s", status, string(body))
	}
	return parseTokenResponse(body)
}

func (c *Client) DeleteToken(ctx context.Context, tokenID string) error {
	body, status, err := c.do(ctx, http.MethodDelete, "/api/v2/authentication-tokens/"+tokenID, nil)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("delete token (HTTP %d): %s", status, string(body))
	}
	return nil
}

func parseTokenResponse(body []byte) (*TokenResponse, error) {
	var resp struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				Token string `json:"token"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &TokenResponse{ID: resp.Data.ID, Token: resp.Data.Attributes.Token}, nil
}
