package openobserve

// TokenPayload is the OpenObserve rotated/dynamic secret payload.
//
// It carries only non-sensitive routing fields plus the issued token. The
// OpenObserve admin credentials the rotator needs to call the management API
// are deliberately NOT part of the payload: they are read from the rotator
// environment (OPENOBSERVE_ADMIN_USERNAME / OPENOBSERVE_ADMIN_PASSWORD), so
// they are never stored in, nor returned with, the secret value a consumer
// reads.
//
// Auth model: OpenObserve service accounts authenticate with HTTP Basic auth
// using the account email as the username and the token as the password.
// Service accounts are API-only and cannot sign in to the web UI.
type TokenPayload struct {
	Type         string `json:"type"`                   // "openobserve_token"
	BaseURL      string `json:"base_url"`               // e.g. "http://openobserve.observability.svc:5080"
	Organization string `json:"organization,omitempty"` // org identifier, defaults to "default"
	Email        string `json:"email"`                  // rotate: exact SA email; create: base address a unique per-lease SA is derived from
	Token        string `json:"token,omitempty"`        // issued token, managed by the rotator
}
