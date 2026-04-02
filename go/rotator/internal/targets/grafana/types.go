package grafana

// TokenPayload for Grafana service account token rotation.
type TokenPayload struct {
	Type             string `json:"type"`               // "grafana_token"
	BaseURL          string `json:"base_url"`           // e.g. "https://myorg.grafana.net"
	AdminToken       string `json:"admin_token"`        // Admin API key or SA token
	ServiceAccountID int    `json:"service_account_id"` // SA to create token for
	TokenName        string `json:"token_name"`
	ExpirySeconds    int    `json:"expiry_seconds"` // 0 = no expiry
	TokenID          int    `json:"token_id"`       // Current token ID
	Token            string `json:"token"`          // Current token value
}
