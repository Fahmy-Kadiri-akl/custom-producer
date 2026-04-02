package newrelic

// KeyPayload for New Relic API key rotation via NerdGraph.
type KeyPayload struct {
	Type        string `json:"type"`          // "newrelic_key"
	AdminAPIKey string `json:"admin_api_key"` // User API key with key management permissions
	AccountID   int    `json:"account_id"`    // New Relic account ID
	KeyType     string `json:"key_type"`      // "USER" or "INGEST" (INGEST = license/browser/mobile)
	IngestType  string `json:"ingest_type"`   // "BROWSER" or "LICENSE" (only for INGEST keys)
	KeyName     string `json:"key_name"`
	KeyID       string `json:"key_id"` // Current key ID (NRAK-...)
	Key         string `json:"key"`    // Current key value
}
