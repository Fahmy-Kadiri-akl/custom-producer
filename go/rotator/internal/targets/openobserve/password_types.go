package openobserve

// PasswordPayload is the OpenObserve user-password rotated/dynamic secret
// payload. Unlike service-account tokens, real OpenObserve users can sign in
// to the web UI, so this variant produces working /web/login credentials.
//
// As with the token variant, the OpenObserve admin credentials are read from
// the rotator environment (OPENOBSERVE_ADMIN_USERNAME / OPENOBSERVE_ADMIN_PASSWORD)
// and are never carried in the payload.
type PasswordPayload struct {
	Type         string `json:"type"`                   // "openobserve_password"
	BaseURL      string `json:"base_url"`               // e.g. "http://openobserve.observability.svc:5080"
	Organization string `json:"organization,omitempty"` // org identifier, defaults to "default"
	Email        string `json:"email"`                  // rotate: exact user; create: base address a unique per-lease user is derived from
	Role         string `json:"role,omitempty"`         // org role, defaults to "admin" (the only built-in role when custom roles are disabled)
	Password     string `json:"password,omitempty"`     // issued password, managed by the rotator
}
