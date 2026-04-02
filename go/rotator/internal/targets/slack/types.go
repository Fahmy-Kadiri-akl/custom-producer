package slack

// TokenPayload for Slack token rotation using the token rotation API.
type TokenPayload struct {
	Type            string `json:"type"`              // "slack_token"
	ClientID        string `json:"client_id"`         // Slack app client ID
	ClientSecret    string `json:"client_secret"`     // Slack app client secret
	RefreshToken    string `json:"refresh_token"`     // Current refresh token
	Token           string `json:"token"`             // Current access token (xoxe-...)
}
