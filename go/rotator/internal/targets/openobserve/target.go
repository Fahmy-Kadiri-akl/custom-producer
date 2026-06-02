package openobserve

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/akeylesslabs/custom-producer/go/pkg/types"
	"github.com/rs/zerolog/log"
)

type Target struct{}

func New() *Target             { return &Target{} }
func (t *Target) Type() string { return "openobserve_token" }

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
	org := p.Organization
	if org == "" {
		org = "default"
	}
	if p.Email == "" {
		return nil, fmt.Errorf("email is required")
	}
	client := NewClient(p.BaseURL, p.AdminUsername, p.AdminPassword)
	email := ephemeralEmail(p.Email)
	if err := client.CreateServiceAccount(ctx, org, email, p.FirstName, p.LastName); err != nil {
		return nil, fmt.Errorf("create service account: %w", err)
	}
	token, err := client.GetToken(ctx, org, email)
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}
	log.Info().Str("email", email).Str("org", org).Msg("created ephemeral OpenObserve service account")
	resp, _ := json.Marshal(map[string]string{
		"email":        email,
		"token":        token,
		"base_url":     p.BaseURL,
		"organization": org,
	})
	return &types.CreateResponse{ID: email, Response: string(resp)}, nil
}

// Revoke handles /sync/revoke for the dynamic-secret model: it deletes the
// ephemeral service accounts identified by the lease IDs from Create.
func (t *Target) Revoke(ctx context.Context, req *types.RevokeRequest) (*types.RevokeResponse, error) {
	var p TokenPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	org := p.Organization
	if org == "" {
		org = "default"
	}
	client := NewClient(p.BaseURL, p.AdminUsername, p.AdminPassword)
	revoked := make([]string, 0, len(req.IDs))
	for _, id := range req.IDs {
		if err := client.DeleteServiceAccount(ctx, org, id); err != nil {
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
