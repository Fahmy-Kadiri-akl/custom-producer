package github

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/akeylesslabs/custom-producer/go/pkg/types"
	"github.com/rs/zerolog/log"
)

type Target struct{}

func New() *Target             { return &Target{} }
func (t *Target) Type() string { return "github_pat" }

func (t *Target) Create(_ context.Context, req *types.CreateRequest) (*types.CreateResponse, error) {
	var p PATPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	resp, _ := json.Marshal(map[string]interface{}{"token": p.Token, "token_id": p.TokenID})
	return &types.CreateResponse{ID: fmt.Sprintf("%d", p.TokenID), Response: string(resp)}, nil
}

func (t *Target) Revoke(_ context.Context, req *types.RevokeRequest) (*types.RevokeResponse, error) {
	return &types.RevokeResponse{Revoked: req.IDs, Message: "acknowledged"}, nil
}

func (t *Target) Rotate(ctx context.Context, req *types.RotateRequest) (*types.RotateResponse, error) {
	var p PATPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	client := NewClient(p.AdminToken)
	days := p.ExpiryDays
	if days <= 0 {
		days = 30
	}
	newToken, err := client.CreateFineGrainedPAT(ctx, p.Owner, p.TokenName, p.Repositories, p.Permissions, days)
	if err != nil {
		return nil, fmt.Errorf("create PAT: %w", err)
	}
	log.Info().Int("new_id", newToken.ID).Str("owner", p.Owner).Msg("new GitHub PAT created")
	if p.TokenID > 0 {
		if err := client.RevokeFineGrainedPAT(ctx, p.Owner, p.TokenID); err != nil {
			log.Warn().Err(err).Int("old_id", p.TokenID).Msg("failed to revoke old GitHub PAT")
		} else {
			log.Info().Int("old_id", p.TokenID).Msg("old GitHub PAT revoked")
		}
	}
	p.TokenID = newToken.ID
	p.Token = newToken.Token
	out, _ := json.Marshal(p)
	return &types.RotateResponse{Payload: string(out)}, nil
}
