package servicenow

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/akeylesslabs/custom-producer/go/pkg/types"
	"github.com/rs/zerolog/log"
)

type Target struct{}

func New() *Target             { return &Target{} }
func (t *Target) Type() string { return "servicenow_cred" }

func (t *Target) Create(_ context.Context, req *types.CreateRequest) (*types.CreateResponse, error) {
	var p CredPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	resp, _ := json.Marshal(map[string]string{"client_id": p.ClientID, "client_secret": p.ClientSecret})
	return &types.CreateResponse{ID: p.ClientID, Response: string(resp)}, nil
}

func (t *Target) Revoke(_ context.Context, req *types.RevokeRequest) (*types.RevokeResponse, error) {
	return &types.RevokeResponse{Revoked: req.IDs, Message: "acknowledged"}, nil
}

func (t *Target) Rotate(ctx context.Context, req *types.RotateRequest) (*types.RotateResponse, error) {
	var p CredPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	newSecret, err := GenerateSecret(32)
	if err != nil {
		return nil, fmt.Errorf("generate secret: %w", err)
	}
	client := NewClient(p.InstanceURL, p.AdminUser, p.AdminPass)
	if err := client.RotateClientSecret(ctx, p.AppSysID, newSecret); err != nil {
		return nil, fmt.Errorf("rotate secret: %w", err)
	}
	log.Info().Str("client_id", p.ClientID).Str("instance", p.InstanceURL).Msg("ServiceNow client secret rotated")
	p.ClientSecret = newSecret
	out, _ := json.Marshal(p)
	return &types.RotateResponse{Payload: string(out)}, nil
}
