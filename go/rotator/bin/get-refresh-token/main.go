// get-refresh-token performs OAuth 2.0 device-code flow against Entra ID and
// prints a refresh token to stdout. Intended for one-time bootstrap of the
// Azure DevOps PAT rotator's refresh_token auth mode.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

const (
	deviceCodeURL = "https://login.microsoftonline.com/%s/oauth2/v2.0/devicecode"
	tokenURL      = "https://login.microsoftonline.com/%s/oauth2/v2.0/token"
	defaultScope  = "499b84ac-1321-427f-aa17-267ca6975798/.default offline_access"
)

func main() {
	tenant := flag.String("tenant", "", "Entra tenant ID (required)")
	clientID := flag.String("client-id", "", "app registration client ID (required)")
	scope := flag.String("scope", defaultScope, "OAuth scope")
	flag.Parse()

	if *tenant == "" || *clientID == "" {
		fmt.Fprintln(os.Stderr, "usage: get-refresh-token --tenant <id> --client-id <id> [--scope <s>]")
		os.Exit(2)
	}

	ctx := context.Background()
	dc, err := requestDeviceCode(ctx, *tenant, *clientID, *scope)
	if err != nil {
		fmt.Fprintf(os.Stderr, "device code request failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, dc.Message)

	tok, err := pollForToken(ctx, *tenant, *clientID, dc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "token poll failed: %v\n", err)
		os.Exit(1)
	}
	if tok.RefreshToken == "" {
		fmt.Fprintln(os.Stderr, "no refresh_token in response (did you include offline_access in scope?)")
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "got refresh_token (scope=%q, access_token expires in %ds)\n", tok.Scope, tok.ExpiresIn)
	fmt.Println(tok.RefreshToken)
}

type deviceCodeResp struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
	Message         string `json:"message"`
}

type tokenResp struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int    `json:"expires_in"`
	Scope            string `json:"scope"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func requestDeviceCode(ctx context.Context, tenant, clientID, scope string) (*deviceCodeResp, error) {
	data := url.Values{"client_id": {clientID}, "scope": {scope}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf(deviceCodeURL, tenant), bytes.NewBufferString(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	var dc deviceCodeResp
	if err := json.Unmarshal(body, &dc); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if dc.Interval <= 0 {
		dc.Interval = 5
	}
	return &dc, nil
}

func pollForToken(ctx context.Context, tenant, clientID string, dc *deviceCodeResp) (*tokenResp, error) {
	interval := dc.Interval
	deadline := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(time.Duration(interval) * time.Second)
		data := url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"client_id":   {clientID},
			"device_code": {dc.DeviceCode},
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf(tokenURL, tenant), bytes.NewBufferString(data.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		var tr tokenResp
		if err := json.Unmarshal(body, &tr); err != nil {
			return nil, fmt.Errorf("parse (HTTP %d): %w", resp.StatusCode, err)
		}
		if tr.AccessToken != "" {
			return &tr, nil
		}
		switch tr.Error {
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5
		default:
			return nil, fmt.Errorf("%s: %s", tr.Error, tr.ErrorDescription)
		}
	}
	return nil, fmt.Errorf("device code expired before user completed authentication")
}
