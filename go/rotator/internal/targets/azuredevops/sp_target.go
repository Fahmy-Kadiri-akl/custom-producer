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

	"github.com/akeylesslabs/custom-producer/go/pkg/types"
	"github.com/rs/zerolog/log"
)

// SPTokenTarget handles azuredevops_sp_token rotation: AAD access tokens for
// the Azure DevOps resource, minted via Entra client_credentials.
type SPTokenTarget struct{}

func NewSPTokenTarget() *SPTokenTarget { return &SPTokenTarget{} }
func (t *SPTokenTarget) Type() string  { return "azuredevops_sp_token" }

func (t *SPTokenTarget) Create(_ context.Context, req *types.CreateRequest) (*types.CreateResponse, error) {
	var p SPTokenPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	resp, _ := json.Marshal(map[string]string{
		"token":      p.Token,
		"expires_at": p.ExpiresAt,
	})
	return &types.CreateResponse{
		ID:       fmt.Sprintf("ado-sp-%s-%s", p.TenantID, p.ClientID),
		Response: string(resp),
	}, nil
}

func (t *SPTokenTarget) Revoke(_ context.Context, req *types.RevokeRequest) (*types.RevokeResponse, error) {
	// AAD access tokens cannot be revoked individually before their natural
	// expiry. The acknowledgement is informational only.
	return &types.RevokeResponse{Revoked: req.IDs, Message: "acknowledged (AAD access tokens auto-expire)"}, nil
}

func (t *SPTokenTarget) Rotate(ctx context.Context, req *types.RotateRequest) (*types.RotateResponse, error) {
	var p SPTokenPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	if p.TenantID == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if p.ClientID == "" {
		return nil, fmt.Errorf("client_id is required")
	}
	if p.ClientSecret == "" {
		return nil, fmt.Errorf("client_secret is required")
	}
	scope := p.Scope
	if scope == "" {
		scope = devopsScope
	}

	tokenURL := fmt.Sprintf(azureADTokenURL, p.TenantID)
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {p.ClientID},
		"client_secret": {p.ClientSecret},
		"scope":         {scope},
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build token request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("client_credentials exchange (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("empty access_token in response: %s", string(body))
	}

	p.Token = tr.AccessToken
	p.ExpiresAt = time.Now().UTC().Add(time.Duration(tr.ExpiresIn) * time.Second).Format(time.RFC3339)
	log.Info().
		Str("tenant_id", p.TenantID).
		Str("client_id", p.ClientID).
		Str("scope", scope).
		Int("expires_in", tr.ExpiresIn).
		Msg("minted Azure DevOps SP access token")

	out, _ := json.Marshal(p)
	return &types.RotateResponse{Payload: string(out)}, nil
}
