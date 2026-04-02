package github

// PATPayload for GitHub PAT rotation (fine-grained or classic).
type PATPayload struct {
	Type         string   `json:"type"`          // "github_pat"
	AdminToken   string   `json:"admin_token"`   // Admin PAT with admin:org scope (for fine-grained PAT mgmt)
	Owner        string   `json:"owner"`         // Org or user that owns the PATs
	TokenName    string   `json:"token_name"`    // Display name for the new token
	Repositories []string `json:"repositories"`  // Repo names (fine-grained)
	Permissions  map[string]string `json:"permissions"` // e.g. {"contents":"read","pull_requests":"write"}
	ExpiryDays   int      `json:"expiry_days"`   // Token TTL in days
	TokenID      int      `json:"token_id"`      // Current fine-grained PAT ID
	Token        string   `json:"token"`         // Current token value
}
