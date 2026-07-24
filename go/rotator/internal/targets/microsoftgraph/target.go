package microsoftgraph

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/akeylesslabs/custom-producer/go/pkg/types"
	"github.com/rs/zerolog/log"
)

const (
	// secretValidityDays is how long a freshly added secret stays valid. It is
	// intentionally longer than the rotation interval so a single failed
	// rotation does not expire the live credential before the next attempt.
	secretValidityDays = 14
	defaultDisplayName = "akeyless-rotated"
)

// Target rotates the client secret of an Entra ID application registration via
// the Microsoft Graph addPassword / removePassword API. The rotator
// authenticates as a dedicated app registration whose identity and certificate
// live in the rotator environment, never in the payload.
type Target struct {
	tenantID string
	clientID string
	certRaw  string
	keyRaw   string
}

func New() *Target {
	return &Target{
		tenantID: os.Getenv("MSGRAPH_ROTATOR_TENANT_ID"),
		clientID: os.Getenv("MSGRAPH_ROTATOR_CLIENT_ID"),
		certRaw:  os.Getenv("MSGRAPH_ROTATOR_CERT"),
		keyRaw:   os.Getenv("MSGRAPH_ROTATOR_KEY"),
	}
}

func (t *Target) Type() string { return "microsoft_graph_app_secret" }

// client builds a Graph Client from the rotator environment. certRaw/keyRaw
// may be either inline PEM or a path to a PEM file (mounted k8s secret).
func (t *Target) client(payloadTenant string) (*Client, error) {
	if t.tenantID == "" || t.clientID == "" {
		return nil, fmt.Errorf("MSGRAPH_ROTATOR_TENANT_ID and MSGRAPH_ROTATOR_CLIENT_ID must be set in the rotator environment")
	}
	if payloadTenant != "" && payloadTenant != t.tenantID {
		return nil, fmt.Errorf("payload tenant_id %q does not match rotator tenant %q", payloadTenant, t.tenantID)
	}
	certPEM, err := resolvePEM("MSGRAPH_ROTATOR_CERT", t.certRaw)
	if err != nil {
		return nil, err
	}
	keyPEM, err := resolvePEM("MSGRAPH_ROTATOR_KEY", t.keyRaw)
	if err != nil {
		return nil, err
	}
	return NewClient(t.tenantID, t.clientID, certPEM, keyPEM)
}

// resolvePEM returns inline PEM verbatim, or reads a PEM file when the value
// looks like a filesystem path.
func resolvePEM(name, v string) (string, error) {
	if v == "" {
		return "", fmt.Errorf("%s is not set in the rotator environment", name)
	}
	if strings.HasPrefix(v, "/") {
		b, err := os.ReadFile(v)
		if err != nil {
			return "", fmt.Errorf("read %s at %s: %w", name, v, err)
		}
		return string(b), nil
	}
	return v, nil
}

// Create handles /sync/create. It returns the current payload state; the first
// Rotate adds a Graph-issued secret and records its keyId. The response must be
// a JSON object (map), not a string, or Akeyless get-value fails.
func (t *Target) Create(_ context.Context, req *types.CreateRequest) (*types.CreateResponse, error) {
	var p AppSecretPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	if p.ClientID == "" {
		return nil, fmt.Errorf("client_id is required")
	}
	if p.Type == "" {
		p.Type = "microsoft_graph_app_secret"
	}
	resp := map[string]string{
		"type":          p.Type,
		"tenant_id":     p.TenantID,
		"client_id":     p.ClientID,
		"client_secret": p.ClientSecret,
		"key_id":        p.KeyID,
		"expires_at":    p.ExpiresAt,
	}
	return &types.CreateResponse{
		ID:       fmt.Sprintf("msgraph-app-%s", p.ClientID),
		Response: resp,
	}, nil
}

// Revoke handles /sync/revoke. It removes the current client secret from the
// target app so the credential does not linger after the item is deleted.
// Failure to remove is best-effort: the item is still acknowledged so Akeyless
// can clean it up.
func (t *Target) Revoke(ctx context.Context, req *types.RevokeRequest) (*types.RevokeResponse, error) {
	var p AppSecretPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return &types.RevokeResponse{Revoked: req.IDs, Message: "acknowledged (unparseable payload)"}, nil
	}
	if p.KeyID == "" || p.ClientID == "" {
		return &types.RevokeResponse{Revoked: req.IDs, Message: "acknowledged (no client_id/key_id to remove)"}, nil
	}
	client, err := t.client(p.TenantID)
	if err != nil {
		return nil, err
	}
	if err := client.removePassword(ctx, p.ClientID, p.KeyID); err != nil {
		log.Warn().Err(err).Str("client_id", p.ClientID).Str("key_id", p.KeyID).Msg("failed to remove Graph password on revoke")
		return &types.RevokeResponse{Message: "acknowledged (removePassword failed, see logs)"}, nil
	}
	log.Info().Str("client_id", p.ClientID).Str("key_id", p.KeyID).Msg("removed Graph client secret on revoke")
	return &types.RevokeResponse{Revoked: req.IDs, Message: "removed"}, nil
}

// Rotate handles /sync/rotate. It adds a new secret first (create-before-
// revoke), stores the new value and keyId in the payload, then removes the
// previous secret. If removal fails the rotation still succeeds because the
// new secret is already active.
func (t *Target) Rotate(ctx context.Context, req *types.RotateRequest) (*types.RotateResponse, error) {
	var p AppSecretPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	if p.ClientID == "" {
		return nil, fmt.Errorf("client_id is required")
	}
	if p.Type == "" {
		p.Type = "microsoft_graph_app_secret"
	}
	client, err := t.client(p.TenantID)
	if err != nil {
		return nil, err
	}

	added, err := client.addPassword(ctx, p.ClientID, p.DisplayName, secretValidityDays)
	if err != nil {
		return nil, fmt.Errorf("addPassword: %w", err)
	}

	prevKeyID := p.KeyID
	p.ClientSecret = added.SecretText
	p.KeyID = added.KeyID
	p.ExpiresAt = added.EndDateTime

	if prevKeyID != "" {
		if err := client.removePassword(ctx, p.ClientID, prevKeyID); err != nil {
			log.Warn().Err(err).Str("client_id", p.ClientID).Str("key_id", prevKeyID).Msg("failed to remove previous Graph password; new secret is already active")
		}
	}

	out, _ := json.Marshal(p)
	log.Info().
		Str("client_id", p.ClientID).
		Str("key_id", p.KeyID).
		Int("validity_days", secretValidityDays).
		Msg("rotated Microsoft Graph application client secret")
	return &types.RotateResponse{Payload: string(out)}, nil
}
