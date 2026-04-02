package sendgrid

// KeyPayload for SendGrid API key rotation.
type KeyPayload struct {
	Type       string   `json:"type"`        // "sendgrid_key"
	AdminKey   string   `json:"admin_key"`   // Existing API key with full access
	KeyName    string   `json:"key_name"`
	Scopes     []string `json:"scopes"`      // e.g. ["mail.send","alerts.read"]
	KeyID      string   `json:"key_id"`      // Current key ID
	Key        string   `json:"key"`         // Current API key value
}
