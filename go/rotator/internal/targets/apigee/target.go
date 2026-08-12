package apigee

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/akeylesslabs/custom-producer/go/pkg/types"
	"github.com/rs/zerolog/log"
)

// TokenTarget handles apigee_x_token rotation. Each rotation exchanges a
// GCP service-account key for a short-lived OAuth2 access token scoped to the
// Apigee X management API. The service-account key is read from the rotator
// environment, not from the payload, so it never appears in the value that
// secret consumers read.
type TokenTarget struct{}

// New creates the apigee_x_token target.
func New() *TokenTarget { return &TokenTarget{} }

// Type returns the payload type string handled by this target.
func (t *TokenTarget) Type() string { return "apigee_x_token" }

// Create returns the credentials currently held in the payload.
func (t *TokenTarget) Create(_ context.Context, req *types.CreateRequest) (*types.CreateResponse, error) {
	var p ApigeeTokenPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	resp, _ := json.Marshal(map[string]string{
		"token":      p.Token,
		"expires_at": p.ExpiresAt,
	})
	return &types.CreateResponse{
		ID:       idFor(&p),
		Response: string(resp),
	}, nil
}

// Revoke acknowledges revocation. GCP OAuth2 access tokens are short-lived
// and cannot be revoked individually before their natural expiry, so this
// call is informational only.
func (t *TokenTarget) Revoke(_ context.Context, req *types.RevokeRequest) (*types.RevokeResponse, error) {
	return &types.RevokeResponse{
		Revoked: req.IDs,
		Message: "acknowledged (GCP access tokens auto-expire)",
	}, nil
}

// Rotate mints a fresh access token and returns the updated payload. The
// service-account key is resolved from the rotator environment via
// p.ServiceAccountRef, so the returned payload stays free of long-lived
// secrets.
func (t *TokenTarget) Rotate(ctx context.Context, req *types.RotateRequest) (*types.RotateResponse, error) {
	var p ApigeeTokenPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	saJSON, err := resolveServiceAccount(p.ServiceAccountRef)
	if err != nil {
		return nil, err
	}

	scope := effectiveScope(p.Scope)
	token, expiresIn, err := mintAccessToken(ctx, saJSON, scope)
	if err != nil {
		return nil, fmt.Errorf("mint apigee access token: %w", err)
	}

	p.Token = token
	p.ExpiresAt = time.Now().UTC().Add(time.Duration(expiresIn) * time.Second).Format(time.RFC3339)
	log.Info().
		Str("organization", p.Organization).
		Str("scope", scope).
		Int("expires_in", expiresIn).
		Msg("minted Apigee X access token")

	out, _ := json.Marshal(p)
	return &types.RotateResponse{Payload: string(out)}, nil
}

// idFor derives a stable identifier for the rotated credential from the
// configured organization.
func idFor(p *ApigeeTokenPayload) string {
	org := p.Organization
	if org == "" {
		org = "apigee-x"
	}
	return fmt.Sprintf("apigee-x-%s", org)
}
