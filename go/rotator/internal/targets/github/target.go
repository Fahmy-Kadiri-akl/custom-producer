package github

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/akeylesslabs/custom-producer/go/pkg/types"
	"github.com/rs/zerolog/log"
)

type Target struct{}

func New() *Target             { return &Target{} }
func (t *Target) Type() string { return "github_app_token" }

func (t *Target) Create(_ context.Context, req *types.CreateRequest) (*types.CreateResponse, error) {
	var p AppTokenPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	resp, _ := json.Marshal(map[string]interface{}{
		"token":      p.Token,
		"expires_at": p.ExpiresAt,
	})
	return &types.CreateResponse{
		ID:       fmt.Sprintf("github-app-%d-installation-%d", p.AppID, p.InstallationID),
		Response: string(resp),
	}, nil
}

func (t *Target) Revoke(ctx context.Context, req *types.RevokeRequest) (*types.RevokeResponse, error) {
	var p AppTokenPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return &types.RevokeResponse{Revoked: req.IDs, Message: "acknowledged (payload not parseable)"}, nil
	}
	if p.Token != "" {
		if err := RevokeInstallationToken(ctx, p.Token); err != nil {
			log.Warn().Err(err).Msg("failed to revoke installation token; will auto-expire")
		}
	}
	return &types.RevokeResponse{Revoked: req.IDs, Message: "acknowledged"}, nil
}

func (t *Target) Rotate(ctx context.Context, req *types.RotateRequest) (*types.RotateResponse, error) {
	var p AppTokenPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	if p.AppID == 0 {
		return nil, fmt.Errorf("app_id is required")
	}
	if p.InstallationID == 0 {
		return nil, fmt.Errorf("installation_id is required")
	}
	if p.PrivateKey == "" {
		return nil, fmt.Errorf("private_key is required")
	}

	tok, err := MintInstallationToken(ctx, p.AppID, p.InstallationID, p.PrivateKey, p.Repositories, p.RepositoryIDs, p.Permissions)
	if err != nil {
		return nil, fmt.Errorf("mint installation token: %w", err)
	}
	log.Info().
		Int64("app_id", p.AppID).
		Int64("installation_id", p.InstallationID).
		Str("expires_at", tok.ExpiresAt).
		Msg("minted GitHub App installation token")

	// Best-effort revoke of the previous token. New token is already active,
	// so even if revoke fails the rotation is successful.
	if p.Token != "" {
		if err := RevokeInstallationToken(ctx, p.Token); err != nil {
			log.Warn().Err(err).Msg("failed to revoke previous installation token; will auto-expire")
		}
	}

	p.Token = tok.Token
	p.ExpiresAt = tok.ExpiresAt
	out, _ := json.Marshal(p)
	return &types.RotateResponse{Payload: string(out)}, nil
}
