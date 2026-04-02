// Package echo provides a test/validation target that returns the payload
// as-is. Useful for verifying the unified rotator is deployed and reachable
// without requiring any external dependencies.
package echo

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/akeylesslabs/custom-producer/go/pkg/types"
)

// Target is a no-op echo target for testing.
type Target struct{}

// New creates a new echo target.
func New() *Target { return &Target{} }

// Type returns "echo".
func (t *Target) Type() string { return "echo" }

// Create returns the payload as the response.
func (t *Target) Create(_ context.Context, req *types.CreateRequest) (*types.CreateResponse, error) {
	return &types.CreateResponse{
		ID:       fmt.Sprintf("echo-%d", time.Now().UnixNano()),
		Response: req.Payload,
	}, nil
}

// Revoke acknowledges the revocation.
func (t *Target) Revoke(_ context.Context, req *types.RevokeRequest) (*types.RevokeResponse, error) {
	return &types.RevokeResponse{
		Revoked: req.IDs,
		Message: "echo revoke acknowledged",
	}, nil
}

// Rotate returns the payload with a rotated_at timestamp added.
func (t *Target) Rotate(_ context.Context, req *types.RotateRequest) (*types.RotateResponse, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(req.Payload), &payload); err != nil {
		return nil, fmt.Errorf("parse echo payload: %w", err)
	}
	payload["rotated_at"] = time.Now().UTC().Format(time.RFC3339)
	out, _ := json.Marshal(payload)
	return &types.RotateResponse{Payload: string(out)}, nil
}
