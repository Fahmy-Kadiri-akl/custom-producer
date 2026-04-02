// Package types defines the shared Akeyless custom producer request and
// response envelope types. These are the wire formats that Akeyless Gateway
// sends and expects back from any custom producer implementation.
package types

// CreateRequest is sent by Akeyless to /sync/create to generate credentials.
type CreateRequest struct {
	Payload    string     `json:"payload"`
	ClientInfo ClientInfo `json:"client_info"`
}

// CreateResponse is returned by the create operation.
type CreateResponse struct {
	ID       string      `json:"id"`
	Response interface{} `json:"response"`
}

// RevokeRequest is sent by Akeyless to /sync/revoke when credentials expire.
type RevokeRequest struct {
	Payload string   `json:"payload"`
	IDs     []string `json:"ids"`
}

// RevokeResponse confirms which credentials were revoked.
type RevokeResponse struct {
	Revoked []string `json:"revoked"`
	Message string   `json:"message,omitempty"`
}

// RotateRequest is sent by Akeyless to /sync/rotate to rotate credentials.
// Payload is a JSON string containing target-specific configuration and the
// current credential state. Each target defines its own payload schema.
type RotateRequest struct {
	Payload string `json:"payload"`
}

// RotateResponse returns the updated payload to be stored in Akeyless.
type RotateResponse struct {
	Payload string `json:"payload"`
}

// ClientInfo wraps the requesting user's identity from Akeyless.
type ClientInfo struct {
	AccessID  string              `json:"access_id"`
	SubClaims map[string][]string `json:"sub_claims"`
}
