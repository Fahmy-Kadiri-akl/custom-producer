package ansible

// PasswordPayload is the encrypted payload for Ansible user password rotation.
type PasswordPayload struct {
	Type           string `json:"type"`            // "password"
	AnsibleURL     string `json:"ansible_url"`     // AAP/AWX controller URL
	AdminUser      string `json:"admin_user"`      // Admin username for API auth
	AdminPassword  string `json:"admin_password"`  // Admin password for API auth
	TargetUsername string `json:"target_username"` // User whose password to rotate
	TargetUserID   int    `json:"target_user_id"`  // User ID (0 = auto-lookup)
	Password       string `json:"password"`        // Current password
	SkipTLSVerify  bool   `json:"skip_tls_verify"`
}

// APIKeyPayload is the encrypted payload for Ansible API token rotation.
type APIKeyPayload struct {
	Type          string `json:"type"`            // "api_key"
	AnsibleURL    string `json:"ansible_url"`     // AAP/AWX controller URL
	AdminUser     string `json:"admin_user"`      // Admin username for API auth
	AdminPassword string `json:"admin_password"`  // Admin password for API auth
	TargetUserID  int    `json:"target_user_id"`  // User whose token to rotate
	TokenID       int    `json:"token_id"`        // Current token ID (for revocation)
	Token         string `json:"token"`           // Current token value
	TokenScope    string `json:"token_scope"`     // "write" or "read"
	Description   string `json:"description"`     // Token description
	SkipTLSVerify bool   `json:"skip_tls_verify"`
}
