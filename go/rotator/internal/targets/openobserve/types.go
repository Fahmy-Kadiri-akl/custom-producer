package openobserve

// TokenPayload for OpenObserve service-account token rotation.
//
// OpenObserve authenticates service accounts with HTTP Basic auth using the
// account email as the username and the account token as the password. A
// single rotate call (PUT .../service_accounts/{email}?rotateToken=true)
// mints a new token and invalidates the previous one atomically, so there is
// no separate revoke step.
type TokenPayload struct {
	Type          string `json:"type"`           // "openobserve_token"
	BaseURL       string `json:"base_url"`       // e.g. "http://openobserve.observability.svc:5080"
	AdminUsername string `json:"admin_username"` // root/admin user email for the API
	AdminPassword string `json:"admin_password"` // root/admin password
	Organization  string `json:"organization"`   // org identifier, e.g. "default"
	Email         string `json:"email"`          // service-account email whose token is rotated
	FirstName     string `json:"first_name"`     // SA display name, preserved on rotate
	LastName      string `json:"last_name"`      // SA display name, preserved on rotate
	Token         string `json:"token"`          // current token value, managed by rotator
}
