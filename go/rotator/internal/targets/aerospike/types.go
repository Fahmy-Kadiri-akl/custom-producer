package aerospike

// PasswordPayload is the encrypted payload for Aerospike user password rotation.
type PasswordPayload struct {
	Type           string `json:"type"`                      // "aerospike_password"
	Seeds          string `json:"seeds"`                     // "host1:3000,host2:3000"
	TLSName        string `json:"tls_name,omitempty"`        // optional, for TLS-enabled cluster
	AuthMode       string `json:"auth_mode,omitempty"`       // "internal" (default), "external", "pki"
	AdminUser      string `json:"admin_user"`                // admin account used to rotate target
	AdminPassword  string `json:"admin_password"`            // admin accounts password
	TargetUser     string `json:"target_user"`               // user whose password gets rotated
	Password       string `json:"password"`                  // current password (updated in-place on rotate)
	PasswordLength int    `json:"password_length,omitempty"` // default 24
}
