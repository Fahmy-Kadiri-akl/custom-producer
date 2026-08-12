package apigee

// ApigeeTokenPayload is the round-tripped payload for the apigee_x_token
// target. It carries only non-secret per-secret config plus the minted token.
//
// The GCP service-account key is intentionally NOT part of this struct. It
// lives in the rotator deployment environment and is resolved at rotation
// time, so that secret consumers reading this value never see the long-lived
// private key.
type ApigeeTokenPayload struct {
	// Type dispatches the handler. Must be "apigee_x_token".
	Type string `json:"type"`
	// ServiceAccountRef names the environment variable on the rotator that
	// holds the GCP service-account key JSON. Defaults to
	// APIGEE_SERVICE_ACCOUNT_JSON when empty. Use a distinct ref per Apigee
	// org when one rotator serves several service accounts.
	ServiceAccountRef string `json:"service_account_ref,omitempty"`
	// Scope is the OAuth2 scope to request. Defaults to
	// https://www.googleapis.com/auth/cloud-platform when empty.
	Scope string `json:"scope,omitempty"`
	// Organization is the Apigee org the consumer targets. Carried for
	// human and audit context; the token exchange does not read it.
	Organization string `json:"organization,omitempty"`
	// Token is the minted access token. Set by the rotator each rotation.
	Token string `json:"token,omitempty"`
	// ExpiresAt is the RFC3339 time at which Token stops being valid.
	// Set by the rotator.
	ExpiresAt string `json:"expires_at,omitempty"`
}
