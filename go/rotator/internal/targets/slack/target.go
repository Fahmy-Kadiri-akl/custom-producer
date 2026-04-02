package slack

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/akeylesslabs/custom-producer/go/pkg/types"
	"github.com/rs/zerolog/log"
)

type Target struct{}

func New() *Target             { return &Target{} }
func (t *Target) Type() string { return "slack_token" }

func (t *Target) Create(_ context.Context, req *types.CreateRequest) (*types.CreateResponse, error) {
	var p TokenPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	resp, _ := json.Marshal(map[string]string{"token": p.Token})
	return &types.CreateResponse{ID: p.ClientID, Response: string(resp)}, nil
}

func (t *Target) Revoke(_ context.Context, req *types.RevokeRequest) (*types.RevokeResponse, error) {
	return &types.RevokeResponse{Revoked: req.IDs, Message: "acknowledged"}, nil
}

func (t *Target) Rotate(ctx context.Context, req *types.RotateRequest) (*types.RotateResponse, error) {
	var p TokenPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	client := NewClient()
	result, err := client.RotateToken(ctx, p.RefreshToken, p.ClientID, p.ClientSecret)
	if err != nil {
		return nil, fmt.Errorf("rotate token: %w", err)
	}
	log.Info().Str("client_id", p.ClientID).Msg("Slack token rotated")
	p.Token = result.Token
	p.RefreshToken = result.RefreshToken
	out, _ := json.Marshal(p)
	return &types.RotateResponse{Payload: string(out)}, nil
}
