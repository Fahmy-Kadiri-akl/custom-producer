package argocd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/akeylesslabs/custom-producer/go/pkg/types"
	"github.com/rs/zerolog/log"
)

type Target struct{}

func New() *Target             { return &Target{} }
func (t *Target) Type() string { return "argocd_token" }

func (t *Target) Create(_ context.Context, req *types.CreateRequest) (*types.CreateResponse, error) {
	var p TokenPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	resp, _ := json.Marshal(map[string]string{"token": p.Token})
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
	newToken, err := client.CreateToken(ctx, p.Account, p.ExpirySeconds)
	if err != nil {
		return nil, fmt.Errorf("create token: %w", err)
	}
	log.Info().Str("account", p.Account).Msg("new ArgoCD token created")
	if p.TokenID != "" {
		if err := client.DeleteToken(ctx, p.Account, p.TokenID); err != nil {
			log.Warn().Err(err).Str("old_id", p.TokenID).Msg("failed to delete old ArgoCD token")
		} else {
			log.Info().Str("old_id", p.TokenID).Msg("old ArgoCD token deleted")
		}
	}
	p.TokenID = newToken.IssuedAt
	p.Token = newToken.Token
	out, _ := json.Marshal(p)
	return &types.RotateResponse{Payload: string(out)}, nil
}
