package azuredevops

// SPTokenPayload is the rotated-secret configuration for the
// azuredevops_sp_token target. Each rotation exchanges the App
// registration's client_credentials for a fresh AAD access token (~1h)
// scoped to the Azure DevOps resource.
//
// Microsoft does not let service principals create or manage Azure DevOps
// PATs, so this rotator produces AAD access tokens, not PATs. The minted
// token authenticates against the Azure DevOps REST API, and against Git
// over HTTPS when used as the oauth2 password. It cannot be used with the
// PATs Lifecycle API; for PAT minting, use the "pat" rotator with a
// delegated user refresh token.
type SPTokenPayload struct {
	Type         string `json:"type"`          // "azuredevops_sp_token"
	TenantID     string `json:"tenant_id"`     // Entra tenant ID
	ClientID     string `json:"client_id"`     // App registration client ID
	ClientSecret string `json:"client_secret"` // App registration client secret

	// Optional: override the AAD scope. Defaults to the Azure DevOps resource
	// (.default scope). Use a different value only if you intend the token
	// for a different Azure resource.
	Scope string `json:"scope,omitempty"`

	// Optional: documents which ADO organization the consumer talks to. The
	// rotator does not use this field; it is for human/audit context only.
	Organization string `json:"organization,omitempty"`

	// Managed by the rotator. Set by Rotate; do not populate at create time.
	Token     string `json:"token,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}
