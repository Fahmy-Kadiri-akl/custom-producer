package azuredevops

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/akeylesslabs/custom-producer/go/pkg/types"
	"github.com/rs/zerolog/log"
)

// PATTarget handles Azure DevOps PAT rotation (type: "pat").
type PATTarget struct{}

func NewPATTarget() *PATTarget { return &PATTarget{} }
func (t *PATTarget) Type() string { return "pat" }

func (t *PATTarget) Create(_ context.Context, req *types.CreateRequest) (*types.CreateResponse, error) {
	var p PATPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	resp, _ := json.Marshal(map[string]string{
		"token":            p.Token,
		"authorization_id": p.AuthorizationID,
		"organization":     p.Organization,
	})
	return &types.CreateResponse{ID: p.AuthorizationID, Response: string(resp)}, nil
}

func (t *PATTarget) Revoke(_ context.Context, req *types.RevokeRequest) (*types.RevokeResponse, error) {
	return &types.RevokeResponse{Revoked: req.IDs, Message: "acknowledged"}, nil
}

func (t *PATTarget) Rotate(ctx context.Context, req *types.RotateRequest) (*types.RotateResponse, error) {
	var p PATPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}

	client, err := buildClient(&p)
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}

	validDays := p.ValidDays
	if validDays <= 0 {
		validDays = 30
	}
	displayName := p.DisplayName
	if displayName == "" {
		displayName = "akeyless-rotated-pat"
	}
	scope := p.Scope
	if scope == "" {
		scope = "app_token"
	}

	// Create new PAT first (create-before-revoke)
	newPAT, err := client.CreatePAT(ctx, p.Organization, displayName, scope, validDays, p.AllOrgs)
	if err != nil {
		return nil, fmt.Errorf("create PAT: %w", err)
	}
	log.Info().
		Str("new_auth_id", newPAT.AuthorizationID).
		Str("org", p.Organization).
		Msg("new Azure DevOps PAT created")

	// Revoke old PAT (best-effort)
	if p.AuthorizationID != "" {
		if err := client.RevokePAT(ctx, p.Organization, p.AuthorizationID); err != nil {
			log.Warn().Err(err).Str("old_auth_id", p.AuthorizationID).Msg("failed to revoke old PAT")
		} else {
			log.Info().Str("old_auth_id", p.AuthorizationID).Msg("old Azure DevOps PAT revoked")
		}
	}

	p.AuthorizationID = newPAT.AuthorizationID
	p.Token = newPAT.Token
	out, _ := json.Marshal(p)
	return &types.RotateResponse{Payload: string(out)}, nil
}

func buildClient(p *PATPayload) (*Client, error) {
	if p.BearerToken != "" {
		return NewBearerClient(p.BearerToken), nil
	}
	if p.Username != "" && p.Password != "" {
		return NewROPCClient(p.TenantID, p.ClientID, p.Username, p.Password), nil
	}
	return nil, fmt.Errorf("no auth: provide bearer_token or username/password with tenant_id/client_id")
}
