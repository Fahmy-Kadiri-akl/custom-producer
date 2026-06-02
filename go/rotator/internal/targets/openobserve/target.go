package openobserve

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/akeylesslabs/custom-producer/go/pkg/types"
	"github.com/rs/zerolog/log"
)

// Display names for managed service accounts. Cosmetic, but required by the
// OpenObserve service-account API body.
const (
	saFirstName = "akeyless"
	saLastName  = "service-account"
)

// Target rotates and provisions OpenObserve service-account tokens. Admin
// credentials are read from the environment, never from the payload.
type Target struct {
	adminUsername string
	adminPassword string
}

func New() *Target {
	return &Target{
		adminUsername: os.Getenv("OPENOBSERVE_ADMIN_USERNAME"),
		adminPassword: os.Getenv("OPENOBSERVE_ADMIN_PASSWORD"),
	}
}

func (t *Target) Type() string { return "openobserve_token" }

func (t *Target) client(baseURL string) (*Client, error) {
	if t.adminUsername == "" || t.adminPassword == "" {
		return nil, fmt.Errorf("OPENOBSERVE_ADMIN_USERNAME and OPENOBSERVE_ADMIN_PASSWORD must be set in the rotator environment")
	}
	return NewClient(baseURL, t.adminUsername, t.adminPassword), nil
}

func orgOrDefault(o string) string {
	if o == "" {
		return "default"
	}
	return o
}

// ephemeralEmail derives a unique per-lease service-account email from a base
// address by inserting a timestamp into the local part, e.g.
// "akeyless-dyn@sol.local" -> "akeyless-dyn-1733251200000000000@sol.local".
func ephemeralEmail(base string) string {
	suffix := fmt.Sprintf("-%d", time.Now().UnixNano())
	if at := strings.LastIndex(base, "@"); at >= 0 {
		return base[:at] + suffix + base[at:]
	}
	return base + suffix
}

// Create handles /sync/create for the dynamic-secret model: it provisions a
// fresh ephemeral service account per lease and returns its credentials. The
// returned ID is the generated email, which Akeyless hands back to Revoke on
// lease expiry.
func (t *Target) Create(ctx context.Context, req *types.CreateRequest) (*types.CreateResponse, error) {
	var p TokenPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	if p.Email == "" {
		return nil, fmt.Errorf("email is required")
	}
	client, err := t.client(p.BaseURL)
	if err != nil {
		return nil, err
	}
	email := ephemeralEmail(p.Email)
	if err := client.CreateServiceAccount(ctx, orgOrDefault(p.Organization), email, saFirstName, saLastName); err != nil {
		return nil, fmt.Errorf("create service account: %w", err)
	}
	token, err := client.GetToken(ctx, orgOrDefault(p.Organization), email)
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}
	log.Info().Str("email", email).Str("org", orgOrDefault(p.Organization)).Msg("created ephemeral OpenObserve service account")
	// Response must be a JSON object (Akeyless unmarshals it into a map), not a string.
	resp := map[string]string{
		"email":        email,
		"token":        token,
		"base_url":     p.BaseURL,
		"organization": orgOrDefault(p.Organization),
	}
	return &types.CreateResponse{ID: email, Response: resp}, nil
}

// Revoke handles /sync/revoke for the dynamic-secret model: it deletes the
// ephemeral service accounts identified by the lease IDs from Create.
func (t *Target) Revoke(ctx context.Context, req *types.RevokeRequest) (*types.RevokeResponse, error) {
	var p TokenPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	client, err := t.client(p.BaseURL)
	if err != nil {
		return nil, err
	}
	revoked := make([]string, 0, len(req.IDs))
	for _, id := range req.IDs {
		if err := client.DeleteServiceAccount(ctx, orgOrDefault(p.Organization), id); err != nil {
			log.Warn().Err(err).Str("email", id).Msg("failed to delete OpenObserve service account")
			continue
		}
		log.Info().Str("email", id).Msg("deleted ephemeral OpenObserve service account")
		revoked = append(revoked, id)
	}
	return &types.RevokeResponse{Revoked: revoked, Message: "deleted"}, nil
}

// Rotate handles /sync/rotate for the rotated-secret model: it rotates the
// token of an existing service account in place.
func (t *Target) Rotate(ctx context.Context, req *types.RotateRequest) (*types.RotateResponse, error) {
	var p TokenPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	if p.Email == "" {
		return nil, fmt.Errorf("email is required")
	}
	client, err := t.client(p.BaseURL)
	if err != nil {
		return nil, err
	}
	newToken, err := client.RotateToken(ctx, orgOrDefault(p.Organization), p.Email, saFirstName, saLastName)
	if err != nil {
		return nil, fmt.Errorf("rotate token: %w", err)
	}
	log.Info().Str("email", p.Email).Str("org", orgOrDefault(p.Organization)).Msg("rotated OpenObserve service-account token")
	p.Token = newToken.Token
	out, _ := json.Marshal(p)
	return &types.RotateResponse{Payload: string(out)}, nil
}
