package gitlab

// TokenPayload for GitLab PAT rotation.
type TokenPayload struct {
	Type       string   `json:"type"`        // "gitlab_token"
	BaseURL    string   `json:"base_url"`    // e.g. "https://gitlab.com" or self-hosted
	AdminToken string   `json:"admin_token"` // Admin PAT with api scope
	UserID     int      `json:"user_id"`     // User to create PAT for
	TokenName  string   `json:"token_name"`
	Scopes     []string `json:"scopes"` // e.g. ["api","read_repository"]
	ExpiryDays int      `json:"expiry_days"`
	TokenID    int      `json:"token_id"` // Current PAT ID
	Token      string   `json:"token"`    // Current PAT value
}
