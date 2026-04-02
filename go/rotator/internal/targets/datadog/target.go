package datadog

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/akeylesslabs/custom-producer/go/pkg/types"
	"github.com/rs/zerolog/log"
)

type Target struct{}

func New() *Target             { return &Target{} }
func (t *Target) Type() string { return "datadog_key" }

func (t *Target) Create(_ context.Context, req *types.CreateRequest) (*types.CreateResponse, error) {
	var p KeyPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	resp, _ := json.Marshal(map[string]string{"key": p.Key, "key_id": p.KeyID, "key_type": p.KeyType})
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
	client := NewClient(p.Site, p.AdminAPI, p.AdminApp)
	keyType := p.KeyType
	if keyType == "" {
		keyType = "api"
	}
	var newKey *KeyResponse
	var err error
	switch keyType {
	case "api":
		newKey, err = client.CreateAPIKey(ctx, p.KeyName)
	case "application":
		newKey, err = client.CreateAppKey(ctx, p.KeyName)
	default:
		return nil, fmt.Errorf("unknown key_type: %q (use \"api\" or \"application\")", keyType)
	}
	if err != nil {
		return nil, fmt.Errorf("create key: %w", err)
	}
	log.Info().Str("new_id", newKey.ID).Str("key_type", keyType).Msg("new Datadog key created")
	if p.KeyID != "" {
		var delErr error
		switch keyType {
		case "api":
			delErr = client.DeleteAPIKey(ctx, p.KeyID)
		case "application":
			delErr = client.DeleteAppKey(ctx, p.KeyID)
		}
		if delErr != nil {
			log.Warn().Err(delErr).Str("old_id", p.KeyID).Msg("failed to delete old Datadog key")
		} else {
			log.Info().Str("old_id", p.KeyID).Msg("old Datadog key deleted")
		}
	}
	p.KeyID = newKey.ID
	p.Key = newKey.Key
	out, _ := json.Marshal(p)
	return &types.RotateResponse{Payload: string(out)}, nil
}
