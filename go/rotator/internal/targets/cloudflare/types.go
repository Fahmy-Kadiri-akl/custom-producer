package cloudflare

// TokenPayload for Cloudflare API token rotation.
type TokenPayload struct {
	Type       string                   `json:"type"`        // "cloudflare_token"
	AdminToken string                   `json:"admin_token"` // Admin API token with token:edit permission
	TokenName  string                   `json:"token_name"`
	Policies   []map[string]interface{} `json:"policies"`            // Permission groups + resources
	Condition  map[string]interface{}   `json:"condition,omitempty"` // IP filtering, etc.
	TokenID    string                   `json:"token_id"`            // Current token ID
	Token      string                   `json:"token"`               // Current token value
}
