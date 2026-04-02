package ansible

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/akeylesslabs/custom-producer/go/pkg/types"
	"github.com/rs/zerolog/log"
)

const passwordCharset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"

// PasswordTarget handles Ansible user password rotation (type: "password").
type PasswordTarget struct{}

func NewPasswordTarget() *PasswordTarget { return &PasswordTarget{} }
func (t *PasswordTarget) Type() string   { return "password" }

func (t *PasswordTarget) Create(_ context.Context, req *types.CreateRequest) (*types.CreateResponse, error) {
	var p PasswordPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	resp, _ := json.Marshal(map[string]string{"username": p.TargetUsername, "password": p.Password})
	return &types.CreateResponse{ID: p.TargetUsername, Response: string(resp)}, nil
}

func (t *PasswordTarget) Revoke(_ context.Context, req *types.RevokeRequest) (*types.RevokeResponse, error) {
	return &types.RevokeResponse{Revoked: req.IDs, Message: "acknowledged"}, nil
}

func (t *PasswordTarget) Rotate(ctx context.Context, req *types.RotateRequest) (*types.RotateResponse, error) {
	var p PasswordPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	client := NewClient(p.AnsibleURL, p.SkipTLSVerify)

	userID := p.TargetUserID
	if userID == 0 {
		var err error
		userID, err = client.LookupUserByUsername(ctx, p.AdminUser, p.AdminPassword, p.TargetUsername)
		if err != nil {
			return nil, fmt.Errorf("lookup user %q: %w", p.TargetUsername, err)
		}
		log.Info().Int("user_id", userID).Str("username", p.TargetUsername).Msg("resolved user ID")
	}

	newPassword, err := generatePassword(24)
	if err != nil {
		return nil, fmt.Errorf("generate password: %w", err)
	}
	if err := client.UpdateUserPassword(ctx, p.AdminUser, p.AdminPassword, userID, newPassword); err != nil {
		return nil, fmt.Errorf("update password: %w", err)
	}

	log.Info().Str("username", p.TargetUsername).Msg("password rotated on Ansible")
	p.Password = newPassword
	p.TargetUserID = userID
	out, _ := json.Marshal(p)
	return &types.RotateResponse{Payload: string(out)}, nil
}

// APIKeyTarget handles Ansible API token rotation (type: "api_key").
type APIKeyTarget struct{}

func NewAPIKeyTarget() *APIKeyTarget { return &APIKeyTarget{} }
func (t *APIKeyTarget) Type() string { return "api_key" }

func (t *APIKeyTarget) Create(_ context.Context, req *types.CreateRequest) (*types.CreateResponse, error) {
	var p APIKeyPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	resp, _ := json.Marshal(map[string]interface{}{"token": p.Token, "token_id": p.TokenID})
	return &types.CreateResponse{ID: fmt.Sprintf("%d", p.TokenID), Response: string(resp)}, nil
}

func (t *APIKeyTarget) Revoke(_ context.Context, req *types.RevokeRequest) (*types.RevokeResponse, error) {
	return &types.RevokeResponse{Revoked: req.IDs, Message: "acknowledged"}, nil
}

func (t *APIKeyTarget) Rotate(ctx context.Context, req *types.RotateRequest) (*types.RotateResponse, error) {
	var p APIKeyPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	client := NewClient(p.AnsibleURL, p.SkipTLSVerify)

	scope := p.TokenScope
	if scope == "" {
		scope = "write"
	}
	desc := p.Description
	if desc == "" {
		desc = "akeyless-rotated-token"
	}

	// Create new token first (create-before-revoke)
	newToken, err := client.CreatePersonalToken(ctx, p.AdminUser, p.AdminPassword, p.TargetUserID, desc, scope)
	if err != nil {
		return nil, fmt.Errorf("create token: %w", err)
	}
	log.Info().Int("new_token_id", newToken.ID).Int("user_id", p.TargetUserID).Msg("new Ansible token created")

	// Revoke old token (best-effort)
	if p.TokenID > 0 {
		if err := client.RevokeToken(ctx, p.AdminUser, p.AdminPassword, p.TokenID); err != nil {
			log.Warn().Err(err).Int("old_token_id", p.TokenID).Msg("failed to revoke old token")
		} else {
			log.Info().Int("old_token_id", p.TokenID).Msg("old Ansible token revoked")
		}
	}

	p.TokenID = newToken.ID
	p.Token = newToken.Token
	out, _ := json.Marshal(p)
	return &types.RotateResponse{Payload: string(out)}, nil
}

func generatePassword(length int) (string, error) {
	pw := make([]byte, length)
	for i := range pw {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(passwordCharset))))
		if err != nil {
			return "", err
		}
		pw[i] = passwordCharset[idx.Int64()]
	}
	return string(pw), nil
}
