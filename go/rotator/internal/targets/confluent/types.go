package confluent

// KeyPayload for Confluent Cloud API key rotation.
type KeyPayload struct {
	Type           string `json:"type"`            // "confluent_key"
	CloudAPIKey    string `json:"cloud_api_key"`   // Admin cloud API key
	CloudAPISecret string `json:"cloud_api_secret"` // Admin cloud API secret
	Owner          string `json:"owner"`           // Resource ID of owner (sa-xxxxx or u-xxxxx)
	ResourceID     string `json:"resource_id"`     // Cluster/environment to scope to (e.g. lkc-xxxxx)
	Description    string `json:"description"`
	KeyID          string `json:"key_id"`          // Current API key ID
	KeySecret      string `json:"key_secret"`      // Current API key secret
}
