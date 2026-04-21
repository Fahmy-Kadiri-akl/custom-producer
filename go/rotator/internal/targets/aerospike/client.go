// Package aerospike provides an admin client wrapper for Aerospike clusters
// and a password rotation target. Aerospike security (users, roles, passwords)
// is an Enterprise Edition feature; calls against Community Edition return a
// wrapped SECURITY_NOT_ENABLED error so the target can ship EE-shaped today
// and activate end-to-end rotation once an EE feature-key is in place.
package aerospike

import (
	"context"
	"fmt"
	"strings"

	as "github.com/aerospike/aerospike-client-go/v8"
	astypes "github.com/aerospike/aerospike-client-go/v8/types"
)

// Client wraps an Aerospike admin client connection.
type Client struct {
	inner *as.Client
}

// NewClient dials the Aerospike cluster using the supplied admin credentials.
// seeds is a comma-separated list of "host:port" entries (e.g. "10.0.0.1:3000,10.0.0.2:3000").
// authMode may be "internal" (default), "external", or "pki".
func NewClient(seeds, tlsName, authMode, adminUser, adminPassword string) (*Client, error) {
	hosts, err := parseSeeds(seeds, tlsName)
	if err != nil {
		return nil, err
	}
	if len(hosts) == 0 {
		return nil, fmt.Errorf("no seeds provided")
	}

	policy := as.NewClientPolicy()
	policy.User = adminUser
	policy.Password = adminPassword
	policy.AuthMode = parseAuthMode(authMode)

	inner, aerr := as.NewClientWithPolicyAndHost(policy, hosts...)
	if aerr != nil {
		return nil, wrapSecurityError(aerr, "connect to Aerospike cluster")
	}
	return &Client{inner: inner}, nil
}

// ChangePassword invokes the Aerospike admin ChangePassword wire command.
// Returns a wrapped error if the cluster does not have security enabled
// (Community Edition, or Enterprise without a security-enabled feature-key).
func (c *Client) ChangePassword(_ context.Context, user, newPassword string) error {
	if aerr := c.inner.ChangePassword(nil, user, newPassword); aerr != nil {
		return wrapSecurityError(aerr, fmt.Sprintf("change password for user %q", user))
	}
	return nil
}

// Close releases the cluster connection.
func (c *Client) Close() {
	if c.inner != nil {
		c.inner.Close()
	}
}

func parseSeeds(seeds, tlsName string) ([]*as.Host, error) {
	raw := strings.Split(seeds, ",")
	addrs := make([]string, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		addrs = append(addrs, s)
	}
	hosts, aerr := as.NewHosts(addrs...)
	if aerr != nil {
		return nil, fmt.Errorf("parse seeds %q: %s", seeds, aerr.Error())
	}
	if tlsName != "" {
		for _, h := range hosts {
			h.TLSName = tlsName
		}
	}
	return hosts, nil
}

func parseAuthMode(mode string) as.AuthMode {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "external":
		return as.AuthModeExternal
	case "pki":
		return as.AuthModePKI
	default:
		return as.AuthModeInternal
	}
}

func wrapSecurityError(aerr as.Error, op string) error {
	if aerr == nil {
		return nil
	}
	if aerr.Matches(astypes.SECURITY_NOT_ENABLED) {
		return fmt.Errorf("%s: Aerospike security not enabled on cluster — Enterprise Edition (or feature-key-enabled build) required for password rotation: %s", op, aerr.Error())
	}
	return fmt.Errorf("%s: %s", op, aerr.Error())
}
