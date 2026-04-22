// Package azuredevops provides an HTTP client for the Azure DevOps PAT
// Lifecycle Management API and a rotation target for personal access tokens.
package azuredevops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	azureADTokenURL = "https://login.microsoftonline.com/%s/oauth2/v2.0/token"
	devopsScope     = "499b84ac-1321-427f-aa17-267ca6975798/.default"
	refreshScope    = "499b84ac-1321-427f-aa17-267ca6975798/.default offline_access"
	patAPIBase      = "https://vssps.dev.azure.com/%s/_apis/tokens/pats"
	apiVersion      = "7.1-preview.1"
)

// Client communicates with the Azure DevOps PAT Lifecycle API.
type Client struct {
	bearerToken  string
	tenantID     string
	clientID     string
	clientSecret string
	username     string
	password     string
	refreshToken string
	authMode     string // "bearer", "ropc", or "refresh_token"
	httpClient   *http.Client

	// Per-instance access token cache so a single Rotate call (CreatePAT +
	// RevokePAT) only exchanges credentials once. Critical for refresh_token
	// mode where the RT is rolled on each exchange.
	cachedAccessToken string
	cachedTokenExpiry time.Time
}

// PATToken represents an Azure DevOps PAT from the API.
type PATToken struct {
	DisplayName     string `json:"displayName"`
	AuthorizationID string `json:"authorizationId"`
	Token           string `json:"token"`
	ValidTo         string `json:"validTo"`
	Scope           string `json:"scope"`
}

// NewBearerClient creates a client using a direct Azure AD bearer token.
func NewBearerClient(token string) *Client {
	return &Client{bearerToken: token, authMode: "bearer", httpClient: &http.Client{Timeout: 30 * time.Second}}
}

// NewROPCClient creates a client using ROPC flow for service accounts.
func NewROPCClient(tenantID, clientID, username, password string) *Client {
	return &Client{
		tenantID: tenantID, clientID: clientID,
		username: username, password: password,
		authMode: "ropc", httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// NewRefreshTokenClient creates a client using OAuth 2.0 refresh-token flow.
// This is the only viable long-lived auth for PAT minting — service principals
// cannot create PATs, only delegated user tokens can.
func NewRefreshTokenClient(tenantID, clientID, clientSecret, refreshToken string) *Client {
	return &Client{
		tenantID: tenantID, clientID: clientID,
		clientSecret: clientSecret, refreshToken: refreshToken,
		authMode: "refresh_token", httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// CurrentRefreshToken returns the current (possibly rolled) refresh token.
// Callers should persist this after rotation.
func (c *Client) CurrentRefreshToken() string { return c.refreshToken }

func (c *Client) getAccessToken(ctx context.Context) (string, error) {
	if c.authMode == "bearer" {
		return c.bearerToken, nil
	}
	if c.cachedAccessToken != "" && time.Now().Before(c.cachedTokenExpiry) {
		return c.cachedAccessToken, nil
	}

	data := url.Values{"client_id": {c.clientID}}
	switch c.authMode {
	case "ropc":
		data.Set("grant_type", "password")
		data.Set("username", c.username)
		data.Set("password", c.password)
		data.Set("scope", devopsScope)
	case "refresh_token":
		data.Set("grant_type", "refresh_token")
		data.Set("refresh_token", c.refreshToken)
		data.Set("scope", refreshScope)
		if c.clientSecret != "" {
			data.Set("client_secret", c.clientSecret)
		}
	default:
		return "", fmt.Errorf("unknown auth mode: %s", c.authMode)
	}

	tokenURL := fmt.Sprintf(azureADTokenURL, c.tenantID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewBufferString(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request (HTTP %d): %s", resp.StatusCode, string(body))
	}
	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("parse token: %w", err)
	}
	if c.authMode == "refresh_token" && tokenResp.RefreshToken != "" {
		c.refreshToken = tokenResp.RefreshToken
	}
	if tokenResp.ExpiresIn > 60 {
		c.cachedAccessToken = tokenResp.AccessToken
		c.cachedTokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second)
	}
	return tokenResp.AccessToken, nil
}

// CreatePAT creates a new personal access token in Azure DevOps.
func (c *Client) CreatePAT(ctx context.Context, org, displayName, scope string, validDays int, allOrgs bool) (*PATToken, error) {
	token, err := c.getAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}
	validTo := time.Now().UTC().Add(time.Duration(validDays) * 24 * time.Hour)
	payload, _ := json.Marshal(map[string]interface{}{
		"displayName": displayName,
		"scope":       scope,
		"validTo":     validTo.Format("2006-01-02T15:04:05.0000000Z"),
		"allOrgs":     allOrgs,
	})
	apiURL := fmt.Sprintf(patAPIBase+"?api-version=%s", org, apiVersion)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("create PAT (HTTP %d): %s", resp.StatusCode, string(body))
	}
	var result struct {
		PATToken       PATToken `json:"patToken"`
		PATTokenError  string   `json:"patTokenError"`
		PATTokenErrMsg *string  `json:"patTokenErrorMessage"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if result.PATTokenError != "none" {
		errMsg := ""
		if result.PATTokenErrMsg != nil {
			errMsg = *result.PATTokenErrMsg
		}
		return nil, fmt.Errorf("PAT error: %s: %s", result.PATTokenError, errMsg)
	}
	return &result.PATToken, nil
}

// RevokePAT deletes a personal access token by authorization ID.
func (c *Client) RevokePAT(ctx context.Context, org, authorizationID string) error {
	token, err := c.getAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("get access token: %w", err)
	}
	apiURL := fmt.Sprintf(patAPIBase+"?authorizationId=%s&api-version=%s", org, authorizationID, apiVersion)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, apiURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("API request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("revoke PAT (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}
