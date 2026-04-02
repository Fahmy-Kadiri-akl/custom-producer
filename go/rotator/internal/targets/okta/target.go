package okta

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/akeylesslabs/custom-producer/go/pkg/types"
	"github.com/rs/zerolog/log"
)

type Target struct{}

func New() *Target             { return &Target{} }
func (t *Target) Type() string { return "okta_key" }

func (t *Target) Create(_ context.Context, req *types.CreateRequest) (*types.CreateResponse, error) {
	var p KeyPayload
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
	var p KeyPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	client := NewClient(p.OrgURL, p.AdminToken)
	newToken, err := client.CreateToken(ctx, p.TokenName)
	if err != nil {
		return nil, fmt.Errorf("create token: %w", err)
	}
	log.Info().Str("new_id", newToken.ID).Str("org", p.OrgURL).Msg("new Okta token created")
	if p.TokenID != "" {
		if err := client.RevokeToken(ctx, p.TokenID); err != nil {
			log.Warn().Err(err).Str("old_id", p.TokenID).Msg("failed to revoke old Okta token")
		} else {
			log.Info().Str("old_id", p.TokenID).Msg("old Okta token revoked")
		}
	}
	p.TokenID = newToken.ID
	p.Token = newToken.Token
	out, _ := json.Marshal(p)
	return &types.RotateResponse{Payload: string(out)}, nil
}
