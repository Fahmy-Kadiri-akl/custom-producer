package servicenow

// CredPayload for ServiceNow OAuth client credential rotation.
// Rotates the client secret for a ServiceNow OAuth application.
type CredPayload struct {
	Type         string `json:"type"`          // "servicenow_cred"
	InstanceURL  string `json:"instance_url"`  // e.g. "https://myinst.service-now.com"
	AdminUser    string `json:"admin_user"`    // Admin username
	AdminPass    string `json:"admin_pass"`    // Admin password
	ClientID     string `json:"client_id"`     // OAuth app client ID
	ClientName   string `json:"client_name"`   // OAuth app name
	ClientSecret string `json:"client_secret"` // Current client secret
	AppSysID     string `json:"app_sys_id"`    // Sys ID of the OAuth application record
}
