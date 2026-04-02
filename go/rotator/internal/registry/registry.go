// Package registry provides a type-based dispatch mechanism for credential
// rotation targets. Each target registers with a string type key (e.g.,
// "password", "api_key", "pat") and the registry routes requests to the
// matching target based on the "type" field in the Akeyless payload.
package registry

import (
	"context"
	"fmt"

	"github.com/akeylesslabs/custom-producer/go/pkg/types"
)

// Target is the interface that all rotation targets must implement.
type Target interface {
	// Type returns the payload type string this target handles (e.g., "pat").
	Type() string

	// Create handles /sync/create — returns current credentials from payload.
	Create(ctx context.Context, req *types.CreateRequest) (*types.CreateResponse, error)

	// Revoke handles /sync/revoke — acknowledges credential revocation.
	Revoke(ctx context.Context, req *types.RevokeRequest) (*types.RevokeResponse, error)

	// Rotate handles /sync/rotate — performs credential rotation and returns
	// an updated payload with the new credentials.
	Rotate(ctx context.Context, req *types.RotateRequest) (*types.RotateResponse, error)
}

// Registry maps payload type strings to their Target implementations.
type Registry struct {
	targets map[string]Target
}

// New creates an empty registry.
func New() *Registry {
	return &Registry{targets: make(map[string]Target)}
}

// Register adds a target to the registry. Panics if the type is already
// registered (duplicate types indicate a programming error, not a runtime
// condition).
func (r *Registry) Register(t Target) {
	name := t.Type()
	if _, exists := r.targets[name]; exists {
		panic(fmt.Sprintf("duplicate target type: %q", name))
	}
	r.targets[name] = t
}

// Get returns the target for the given type, or false if not found.
func (r *Registry) Get(typeName string) (Target, bool) {
	t, ok := r.targets[typeName]
	return t, ok
}

// Types returns all registered type names.
func (r *Registry) Types() []string {
	out := make([]string, 0, len(r.targets))
	for k := range r.targets {
		out = append(out, k)
	}
	return out
}
