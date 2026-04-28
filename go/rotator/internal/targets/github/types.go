package github

// AppTokenPayload represents the configuration for a GitHub App installation
// access-token rotated secret. Each rotation mints a fresh installation
// access token (~1h lifetime) using the App's private key + installation ID.
//
// GitHub does not expose a REST API to create fine-grained or classic
// personal access tokens, so PAT rotation is not implementable. App
// installation tokens are the supported short-lived credential primitive.
type AppTokenPayload struct {
	Type string `json:"type"` // "github_app_token"

	// Required: identifies the GitHub App and which installation to mint for.
	AppID          int64  `json:"app_id"`
	InstallationID int64  `json:"installation_id"`
	PrivateKey     string `json:"private_key"` // PEM-encoded RSA private key (PKCS#1 or PKCS#8)

	// Optional: scope the minted installation token to a subset of the
	// installation's repositories and/or permissions. Leave empty to mint
	// with the App's full installed access.
	Repositories  []string          `json:"repositories,omitempty"`
	RepositoryIDs []int64           `json:"repository_ids,omitempty"`
	Permissions   map[string]string `json:"permissions,omitempty"`

	// Managed by the rotator — set by Rotate, do not populate at create time.
	Token     string `json:"token,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}
