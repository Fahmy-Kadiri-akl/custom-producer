package grafana

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/akeylesslabs/custom-producer/go/pkg/types"
	"github.com/rs/zerolog/log"
)

type Target struct{}

func New() *Target             { return &Target{} }
func (t *Target) Type() string { return "grafana_token" }

func (t *Target) Create(_ context.Context, req *types.CreateRequest) (*types.CreateResponse, error) {
	var p TokenPayload
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
	var p TokenPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	client := NewClient(p.BaseURL, p.AdminToken)

	// Grafana enforces unique token names — revoke old FIRST, then create new
	if p.TokenID > 0 {
		if err := client.DeleteToken(ctx, p.ServiceAccountID, p.TokenID); err != nil {
			log.Warn().Err(err).Int("old_id", p.TokenID).Msg("failed to delete old Grafana token")
		} else {
			log.Info().Int("old_id", p.TokenID).Msg("old Grafana token deleted")
		}
	}

	uniqueName := fmt.Sprintf("%s-%d", p.TokenName, time.Now().UnixNano())
	newToken, err := client.CreateToken(ctx, p.ServiceAccountID, uniqueName, p.ExpirySeconds)
	if err != nil {
		return nil, fmt.Errorf("create token: %w", err)
	}
	log.Info().Int("new_id", newToken.ID).Int("sa_id", p.ServiceAccountID).Msg("new Grafana token created")
	p.TokenID = newToken.ID
	p.Token = newToken.Key
	out, _ := json.Marshal(p)
	return &types.RotateResponse{Payload: string(out)}, nil
}
