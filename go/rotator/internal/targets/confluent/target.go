package confluent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/akeylesslabs/custom-producer/go/pkg/types"
	"github.com/rs/zerolog/log"
)

type Target struct{}

func New() *Target             { return &Target{} }
func (t *Target) Type() string { return "confluent_key" }

func (t *Target) Create(_ context.Context, req *types.CreateRequest) (*types.CreateResponse, error) {
	var p KeyPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	resp, _ := json.Marshal(map[string]string{"key_id": p.KeyID, "key_secret": p.KeySecret})
	return &types.CreateResponse{ID: p.KeyID, Response: string(resp)}, nil
}

func (t *Target) Revoke(_ context.Context, req *types.RevokeRequest) (*types.RevokeResponse, error) {
	return &types.RevokeResponse{Revoked: req.IDs, Message: "acknowledged"}, nil
}

func (t *Target) Rotate(ctx context.Context, req *types.RotateRequest) (*types.RotateResponse, error) {
	var p KeyPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	client := NewClient(p.CloudAPIKey, p.CloudAPISecret)
	newKey, err := client.CreateAPIKey(ctx, p.Owner, p.ResourceID, p.Description)
	if err != nil {
		return nil, fmt.Errorf("create key: %w", err)
	}
	log.Info().Str("new_id", newKey.ID).Str("owner", p.Owner).Msg("new Confluent API key created")
	if p.KeyID != "" {
		if err := client.DeleteAPIKey(ctx, p.KeyID); err != nil {
			log.Warn().Err(err).Str("old_id", p.KeyID).Msg("failed to delete old Confluent key")
		} else {
			log.Info().Str("old_id", p.KeyID).Msg("old Confluent key deleted")
		}
	}
	p.KeyID = newKey.ID
	p.KeySecret = newKey.Secret
	out, _ := json.Marshal(p)
	return &types.RotateResponse{Payload: string(out)}, nil
}
