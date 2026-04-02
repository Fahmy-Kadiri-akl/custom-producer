package terraform

// TokenPayload for Terraform Cloud/Enterprise API token rotation.
type TokenPayload struct {
	Type        string `json:"type"`        // "tfc_token"
	BaseURL     string `json:"base_url"`    // "https://app.terraform.io" or TFE URL
	AdminToken  string `json:"admin_token"` // Existing token with manage permissions
	TokenType   string `json:"token_type"`  // "team", "organization", or "user"
	TeamID      string `json:"team_id"`     // Required for team tokens
	OrgName     string `json:"org_name"`    // Required for org tokens
	Description string `json:"description"`
	TokenID     string `json:"token_id"` // Current token ID
	Token       string `json:"token"`    // Current token value
}
