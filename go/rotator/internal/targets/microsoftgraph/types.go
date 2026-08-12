package microsoftgraph

// AppSecretPayload is the rotated-secret payload for the
// microsoft_graph_app_secret target. It rotates the client secret of an
// Entra ID (Azure AD) application registration through the Microsoft Graph
// application addPassword / removePassword API.
//
// The rotator does NOT authenticate as the target application. It
// authenticates as a dedicated rotator app registration whose tenant, client
// id, and certificate are configured in the rotator environment
// (MSGRAPH_ROTATOR_TENANT_ID, MSGRAPH_ROTATOR_CLIENT_ID, MSGRAPH_ROTATOR_CERT,
// MSGRAPH_ROTATOR_KEY). That rotator app must hold Application.ReadWrite.OwnedBy
// (or Application.ReadWrite.All) and be an owner of each target application.
//
// Microsoft never returns an existing secret's value after creation, so the
// payload carries the current secret value and its keyId: the value is what
// consumers read, and the keyId lets the next rotation remove it.
type AppSecretPayload struct {
	Type     string `json:"type"`      // "microsoft_graph_app_secret"
	TenantID string `json:"tenant_id"` // target app's Entra tenant; must match the rotator tenant
	ClientID string `json:"client_id"` // target application's appId (client id)

	// Optional display name for the secret as shown in the app registration.
	// Defaults to "akeyless-rotated".
	DisplayName string `json:"display_name,omitempty"`

	// Managed by the rotator. Seed both client_secret and key_id at create time
	// with the target app's current secret; Rotate replaces them each cycle.
	ClientSecret string `json:"client_secret,omitempty"` // live secret value consumers read
	KeyID        string `json:"key_id,omitempty"`        // current secret's keyId, removed next rotation
	ExpiresAt    string `json:"expires_at,omitempty"`    // RFC3339, from addPassword endDateTime
}
