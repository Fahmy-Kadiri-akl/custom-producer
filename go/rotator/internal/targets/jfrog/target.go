package jfrog

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/akeylesslabs/custom-producer/go/pkg/types"
	"github.com/rs/zerolog/log"
)

type Target struct{}

func New() *Target             { return &Target{} }
func (t *Target) Type() string { return "jfrog_token" }

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
	expires := p.ExpiresInSecs
	if expires <= 0 {
		expires = 3600
	}
	newToken, err := client.CreateToken(ctx, p.Username, p.Scope, p.Description, expires)
	if err != nil {
		return nil, fmt.Errorf("create token: %w", err)
	}
	log.Info().Str("new_token_id", newToken.TokenID).Str("user", p.Username).Msg("new JFrog token created")
	if p.TokenID != "" {
		if err := client.RevokeToken(ctx, p.TokenID); err != nil {
			log.Warn().Err(err).Str("old_token_id", p.TokenID).Msg("failed to revoke old JFrog token")
		} else {
			log.Info().Str("old_token_id", p.TokenID).Msg("old JFrog token revoked")
		}
	}
	p.TokenID = newToken.TokenID
	p.Token = newToken.AccessToken
	out, _ := json.Marshal(p)
	return &types.RotateResponse{Payload: string(out)}, nil
}
