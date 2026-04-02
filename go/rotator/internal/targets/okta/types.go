package okta

// KeyPayload for Okta API token (SSWS) rotation.
type KeyPayload struct {
	Type       string `json:"type"`        // "okta_key"
	OrgURL     string `json:"org_url"`     // e.g. "https://myorg.okta.com"
	AdminToken string `json:"admin_token"` // SSWS token with okta.apiTokens.manage
	TokenName  string `json:"token_name"`
	TokenID    string `json:"token_id"`    // Current token ID
	Token      string `json:"token"`       // Current SSWS token value
}
