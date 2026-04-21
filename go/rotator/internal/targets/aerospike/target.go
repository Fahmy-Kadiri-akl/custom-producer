package aerospike

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

// Target rotates Aerospike DB user passwords (type: "aerospike_password").
type Target struct{}

func New() *Target             { return &Target{} }
func (t *Target) Type() string { return "aerospike_password" }

func (t *Target) Create(_ context.Context, req *types.CreateRequest) (*types.CreateResponse, error) {
	var p PasswordPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	resp, _ := json.Marshal(map[string]string{"target_user": p.TargetUser, "password": p.Password})
	return &types.CreateResponse{ID: p.TargetUser, Response: string(resp)}, nil
}

func (t *Target) Revoke(_ context.Context, req *types.RevokeRequest) (*types.RevokeResponse, error) {
	return &types.RevokeResponse{Revoked: req.IDs, Message: "acknowledged"}, nil
}

func (t *Target) Rotate(ctx context.Context, req *types.RotateRequest) (*types.RotateResponse, error) {
	var p PasswordPayload
	if err := json.Unmarshal([]byte(req.Payload), &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}

	length := p.PasswordLength
	if length <= 0 {
		length = 24
	}
	newPassword, err := generatePassword(length)
	if err != nil {
		return nil, fmt.Errorf("generate password: %w", err)
	}

	client, err := NewClient(p.Seeds, p.TLSName, p.AuthMode, p.AdminUser, p.AdminPassword)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	if err := client.ChangePassword(ctx, p.TargetUser, newPassword); err != nil {
		return nil, err
	}

	log.Info().Str("target_user", p.TargetUser).Str("seeds", p.Seeds).Msg("Aerospike password rotated")
	p.Password = newPassword
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
