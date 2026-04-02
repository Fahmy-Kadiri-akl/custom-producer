package terraform

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/akeylesslabs/custom-producer/go/pkg/types"
	"github.com/rs/zerolog/log"
)

type Target struct{}

func New() *Target             { return &Target{} }
func (t *Target) Type() string { return "tfc_token" }

func (t *Target) Create(_ context.Context, req *types.CreateRequest) (*types.CreateResponse, error) {
	var p TokenPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	resp, _ := json.Marshal(map[string]string{"token": p.Token, "token_id": p.TokenID})
	return &types.CreateResponse{ID: p.TokenID, Response: string(resp)}, nil
}

func (t *Target) Revoke(_ context.Context, req *types.RevokeRequest) (*types.RevokeResponse, error) {
	return &types.RevokeResponse{Revoked: req.IDs, Message: "acknowledged"}, nil
}

func (t *Target) Rotate(ctx context.Context, req *types.RotateRequest) (*types.RotateResponse, error) {
	var p TokenPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	client := NewClient(p.BaseURL, p.AdminToken)

	// TFC replaces the token on create (team/org tokens are singleton), so no separate revoke needed
	var newToken *TokenResponse
	var err error
	switch p.TokenType {
	case "team":
		newToken, err = client.CreateTeamToken(ctx, p.TeamID)
	case "organization":
		newToken, err = client.CreateOrgToken(ctx, p.OrgName)
	default:
		return nil, fmt.Errorf("unknown token_type: %q (use \"team\" or \"organization\")", p.TokenType)
	}
	if err != nil {
		return nil, fmt.Errorf("create token: %w", err)
	}
	log.Info().Str("new_id", newToken.ID).Str("token_type", p.TokenType).Msg("new TFC token created")

	// Delete old token if it differs (team/org tokens get replaced, but explicit cleanup is safe)
	if p.TokenID != "" && p.TokenID != newToken.ID {
		if err := client.DeleteToken(ctx, p.TokenID); err != nil {
			log.Warn().Err(err).Str("old_id", p.TokenID).Msg("failed to delete old TFC token")
		}
	}
	p.TokenID = newToken.ID
	p.Token = newToken.Token
	out, _ := json.Marshal(p)
	return &types.RotateResponse{Payload: string(out)}, nil
}
