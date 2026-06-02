package openobserve

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"os"

	"github.com/akeylesslabs/custom-producer/go/pkg/types"
	"github.com/rs/zerolog/log"
)

// defaultRole is the only built-in OpenObserve org role available when custom
// (enterprise RBAC) roles are disabled. UI-login users must have it.
const defaultRole = "admin"

// PasswordTarget rotates and provisions OpenObserve user passwords. Real users
// (unlike service accounts) can sign in to the web UI. Admin credentials are
// read from the environment, never from the payload.
type PasswordTarget struct {
	adminUsername string
	adminPassword string
}

func NewPasswordTarget() *PasswordTarget {
	return &PasswordTarget{
		adminUsername: os.Getenv("OPENOBSERVE_ADMIN_USERNAME"),
		adminPassword: os.Getenv("OPENOBSERVE_ADMIN_PASSWORD"),
	}
}

func (t *PasswordTarget) Type() string { return "openobserve_password" }

func (t *PasswordTarget) client(baseURL string) (*Client, error) {
	if t.adminUsername == "" || t.adminPassword == "" {
		return nil, fmt.Errorf("OPENOBSERVE_ADMIN_USERNAME and OPENOBSERVE_ADMIN_PASSWORD must be set in the rotator environment")
	}
	return NewClient(baseURL, t.adminUsername, t.adminPassword), nil
}

func roleOrDefault(r string) string {
	if r == "" {
		return defaultRole
	}
	return r
}

// generatePassword returns a 24-character password with at least one lower,
// upper, digit and special character, drawn from crypto/rand.
func generatePassword() (string, error) {
	const (
		lower   = "abcdefghijkmnopqrstuvwxyz"
		upper   = "ABCDEFGHJKLMNPQRSTUVWXYZ"
		digit   = "23456789"
		special = "!@#$%*-_"
	)
	classes := []string{lower, upper, digit, special}
	all := lower + upper + digit + special
	const length = 24

	pick := func(set string) (byte, error) {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(set))))
		if err != nil {
			return 0, err
		}
		return set[n.Int64()], nil
	}

	out := make([]byte, length)
	// Guarantee one character from each class.
	for i, set := range classes {
		c, err := pick(set)
		if err != nil {
			return "", err
		}
		out[i] = c
	}
	for i := len(classes); i < length; i++ {
		c, err := pick(all)
		if err != nil {
			return "", err
		}
		out[i] = c
	}
	// Fisher-Yates shuffle so the guaranteed characters are not positional.
	for i := length - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", err
		}
		out[i], out[j.Int64()] = out[j.Int64()], out[i]
	}
	return string(out), nil
}

// Create handles /sync/create for the dynamic-secret model: it provisions a
// fresh ephemeral UI user per lease and returns its login credentials.
func (t *PasswordTarget) Create(ctx context.Context, req *types.CreateRequest) (*types.CreateResponse, error) {
	var p PasswordPayload
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
	password, err := generatePassword()
	if err != nil {
		return nil, fmt.Errorf("generate password: %w", err)
	}
	if err := client.CreateUser(ctx, orgOrDefault(p.Organization), email, password, saFirstName, saLastName, roleOrDefault(p.Role)); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	log.Info().Str("email", email).Str("org", orgOrDefault(p.Organization)).Msg("created ephemeral OpenObserve UI user")
	// Response must be a JSON object (Akeyless unmarshals it into a map), not a string.
	resp := map[string]string{
		"email":        email,
		"password":     password,
		"base_url":     p.BaseURL,
		"organization": orgOrDefault(p.Organization),
	}
	return &types.CreateResponse{ID: email, Response: resp}, nil
}

// Revoke handles /sync/revoke for the dynamic-secret model: it deletes the
// ephemeral users identified by the lease IDs from Create.
func (t *PasswordTarget) Revoke(ctx context.Context, req *types.RevokeRequest) (*types.RevokeResponse, error) {
	var p PasswordPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	client, err := t.client(p.BaseURL)
	if err != nil {
		return nil, err
	}
	revoked := make([]string, 0, len(req.IDs))
	for _, id := range req.IDs {
		if err := client.DeleteUser(ctx, orgOrDefault(p.Organization), id); err != nil {
			log.Warn().Err(err).Str("email", id).Msg("failed to delete OpenObserve user")
			continue
		}
		log.Info().Str("email", id).Msg("deleted ephemeral OpenObserve UI user")
		revoked = append(revoked, id)
	}
	return &types.RevokeResponse{Revoked: revoked, Message: "deleted"}, nil
}

// Rotate handles /sync/rotate for the rotated-secret model: it sets a new
// password on an existing user in place.
func (t *PasswordTarget) Rotate(ctx context.Context, req *types.RotateRequest) (*types.RotateResponse, error) {
	var p PasswordPayload
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
	password, err := generatePassword()
	if err != nil {
		return nil, fmt.Errorf("generate password: %w", err)
	}
	if err := client.SetUserPassword(ctx, orgOrDefault(p.Organization), p.Email, password, saFirstName, saLastName, roleOrDefault(p.Role)); err != nil {
		return nil, fmt.Errorf("set user password: %w", err)
	}
	log.Info().Str("email", p.Email).Str("org", orgOrDefault(p.Organization)).Msg("rotated OpenObserve user password")
	p.Password = password
	out, _ := json.Marshal(p)
	return &types.RotateResponse{Payload: string(out)}, nil
}
