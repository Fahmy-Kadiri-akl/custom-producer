package argocd

// TokenPayload for ArgoCD account token rotation.
type TokenPayload struct {
	Type        string `json:"type"`         // "argocd_token"
	BaseURL     string `json:"base_url"`     // e.g. "https://argocd.example.com"
	AdminToken  string `json:"admin_token"`  // Admin bearer token or JWT
	Account     string `json:"account"`      // Account name to generate token for
	ExpirySeconds int  `json:"expiry_seconds"` // Token TTL (0 = no expiry)
	TokenID     string `json:"token_id"`     // Current token ID (iat-based)
	Token       string `json:"token"`        // Current JWT token
}
