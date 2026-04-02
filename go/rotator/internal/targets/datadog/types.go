package datadog

// KeyPayload for Datadog API key or application key rotation.
type KeyPayload struct {
	Type     string `json:"type"`      // "datadog_key"
	Site     string `json:"site"`      // e.g. "datadoghq.com", "us5.datadoghq.com", "datadoghq.eu"
	KeyType  string `json:"key_type"`  // "api" or "application"
	AdminAPI string `json:"admin_api"` // Existing API key for auth
	AdminApp string `json:"admin_app"` // Existing application key for auth
	KeyName  string `json:"key_name"`  // Display name
	KeyID    string `json:"key_id"`    // Current key ID
	Key      string `json:"key"`       // Current key value
}
