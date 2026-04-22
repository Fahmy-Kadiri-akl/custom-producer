package azuredevops

// PATPayload is the encrypted payload for Azure DevOps PAT rotation.
type PATPayload struct {
	Type         string `json:"type"`         // "pat"
	Organization string `json:"organization"` // Azure DevOps org name
	DisplayName  string `json:"display_name"` // PAT display name
	Scope        string `json:"scope"`        // PAT scope (e.g. "app_token")
	ValidDays    int    `json:"valid_days"`   // PAT validity in days
	AllOrgs      bool   `json:"all_orgs"`     // Apply to all organizations

	// Auth: direct bearer token (for testing / user Azure AD token)
	BearerToken string `json:"bearer_token,omitempty"`

	// Auth: ROPC flow (for service accounts without MFA)
	TenantID string `json:"tenant_id,omitempty"`
	ClientID string `json:"client_id,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`

	// Auth: refresh-token flow (delegated user, long-lived)
	RefreshToken string `json:"refresh_token,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"` // optional, confidential clients only

	// Current PAT state (updated by rotation)
	AuthorizationID string `json:"authorization_id"`
	Token           string `json:"token"`
}
