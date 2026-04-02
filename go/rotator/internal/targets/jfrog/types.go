package jfrog

// TokenPayload for JFrog Artifactory access token rotation.
type TokenPayload struct {
	Type          string `json:"type"`            // "jfrog_token"
	BaseURL       string `json:"base_url"`        // e.g. "https://mycompany.jfrog.io"
	AdminToken    string `json:"admin_token"`     // Admin access token for API auth
	Username      string `json:"username"`        // Subject user for the token
	Scope         string `json:"scope"`           // e.g. "applied-permissions/user"
	ExpiresInSecs int    `json:"expires_in_secs"` // Token TTL (0 = non-expiring)
	Description   string `json:"description"`
	TokenID       string `json:"token_id"` // Current token ID (for revocation)
	Token         string `json:"token"`    // Current access token value
}
