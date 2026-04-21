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
	if err := preflight(seeds, tlsName); err != nil {
		return nil, err
	}

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

// preflight opens a short-lived unauthenticated raw connection to the first
// reachable seed and inspects the "build-edition" and "features" info fields.
// It returns a clean human-readable error if the cluster is Community Edition
// or lacks the "security" feature. It returns a connect-error wrapped with
// context if no seed is reachable — network/TLS failures must NOT be
// interpreted as "not EE".
//
// This deliberately uses as.NewConnection (raw TCP + info protocol) rather
// than as.NewClientWithPolicyAndHost, because the full cluster-join handshake
// can itself fail opaquely against CE (e.g. "buffer size invalid") before we
// ever get a chance to read an info field.
func preflight(seeds, tlsName string) error {
	hosts, err := parseSeeds(seeds, tlsName)
	if err != nil {
		return err
	}
	if len(hosts) == 0 {
		return fmt.Errorf("no seeds provided")
	}

	policy := as.NewClientPolicy()
	if tlsName != "" {
		policy.TlsConfig = nil // caller-provided TLS is out of scope for the stub probe
	}

	var (
		info        map[string]string
		lastConnErr error
	)
	for _, h := range hosts {
		conn, aerr := as.NewConnection(policy, h)
		if aerr != nil {
			lastConnErr = fmt.Errorf("dial %s:%d: %s", h.Name, h.Port, aerr.Error())
			continue
		}
		m, ierr := conn.RequestInfo("build-edition", "features")
		conn.Close()
		if ierr != nil {
			lastConnErr = fmt.Errorf("info probe on %s:%d: %s", h.Name, h.Port, ierr.Error())
			continue
		}
		info = m
		lastConnErr = nil
		break
	}
	if info == nil {
		msg := "no reachable seeds"
		if lastConnErr != nil {
			msg = lastConnErr.Error()
		}
		return fmt.Errorf("preflight connect to Aerospike cluster: %s", msg)
	}

	edition := info["build-edition"]
	features := info["features"]
	if !strings.Contains(strings.ToLower(edition), "enterprise") || !strings.Contains(features, "security") {
		return fmt.Errorf("Aerospike security not available: cluster reports build-edition=%q (features=%q). Enterprise Edition with security enabled is required for password rotation.", edition, features)
	}
	return nil
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
