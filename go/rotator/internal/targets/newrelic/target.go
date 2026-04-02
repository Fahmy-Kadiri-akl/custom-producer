package newrelic

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/akeylesslabs/custom-producer/go/pkg/types"
	"github.com/rs/zerolog/log"
)

type Target struct{}

func New() *Target             { return &Target{} }
func (t *Target) Type() string { return "newrelic_key" }

func (t *Target) Create(_ context.Context, req *types.CreateRequest) (*types.CreateResponse, error) {
	var p KeyPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	resp, _ := json.Marshal(map[string]string{"key": p.Key, "key_id": p.KeyID})
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
	client := NewClient(p.AdminAPIKey)
	var newKey *KeyResponse
	var err error
	switch p.KeyType {
	case "USER", "":
		newKey, err = client.CreateUserKey(ctx, p.AccountID, p.KeyName)
	case "INGEST":
		ingestType := p.IngestType
		if ingestType == "" {
			ingestType = "LICENSE"
		}
		newKey, err = client.CreateIngestKey(ctx, p.AccountID, p.KeyName, ingestType)
	default:
		return nil, fmt.Errorf("unknown key_type: %q (use \"USER\" or \"INGEST\")", p.KeyType)
	}
	if err != nil {
		return nil, fmt.Errorf("create key: %w", err)
	}
	log.Info().Str("new_id", newKey.ID).Str("key_type", p.KeyType).Msg("new New Relic key created")
	if p.KeyID != "" {
		if err := client.DeleteKey(ctx, p.KeyID); err != nil {
			log.Warn().Err(err).Str("old_id", p.KeyID).Msg("failed to delete old New Relic key")
		} else {
			log.Info().Str("old_id", p.KeyID).Msg("old New Relic key deleted")
		}
	}
	p.KeyID = newKey.ID
	p.Key = newKey.Key
	out, _ := json.Marshal(p)
	return &types.RotateResponse{Payload: string(out)}, nil
}
