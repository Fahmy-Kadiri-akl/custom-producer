package pagerduty

// KeyPayload for PagerDuty API key rotation.
// PagerDuty v2 API keys are account-level and managed via the REST API.
type KeyPayload struct {
	Type        string `json:"type"`         // "pagerduty_key"
	AdminToken  string `json:"admin_token"`  // Existing API key with full access
	KeyName     string `json:"key_name"`     // Description for the new key
	KeyType     string `json:"key_type"`     // "read_only" or "full_access"
	KeyID       string `json:"key_id"`       // Current key ID
	Key         string `json:"key"`          // Current key value
}
