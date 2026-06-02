package openobserve

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/akeylesslabs/custom-producer/go/pkg/types"
	"github.com/rs/zerolog/log"
)

type Target struct{}

func New() *Target             { return &Target{} }
func (t *Target) Type() string { return "openobserve_token" }

func (t *Target) Create(_ context.Context, req *types.CreateRequest) (*types.CreateResponse, error) {
	var p TokenPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	resp, _ := json.Marshal(map[string]string{"token": p.Token, "email": p.Email})
	return &types.CreateResponse{ID: p.Email, Response: string(resp)}, nil
}

func (t *Target) Revoke(_ context.Context, req *types.RevokeRequest) (*types.RevokeResponse, error) {
	return &types.RevokeResponse{Revoked: req.IDs, Message: "acknowledged"}, nil
}

func (t *Target) Rotate(ctx context.Context, req *types.RotateRequest) (*types.RotateResponse, error) {
	var p TokenPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	org := p.Organization
	if org == "" {
		org = "default"
	}
	if p.Email == "" {
		return nil, fmt.Errorf("email is required")
	}
	client := NewClient(p.BaseURL, p.AdminUsername, p.AdminPassword)
	newToken, err := client.RotateToken(ctx, org, p.Email, p.FirstName, p.LastName)
	if err != nil {
		return nil, fmt.Errorf("rotate token: %w", err)
	}
	log.Info().Str("email", p.Email).Str("org", org).Msg("rotated OpenObserve service-account token")
	p.Token = newToken.Token
	out, _ := json.Marshal(p)
	return &types.RotateResponse{Payload: string(out)}, nil
}
