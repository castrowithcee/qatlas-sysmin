package control

// Problem is one finding of the validation: where it is, which rule it breaks, why, and what to do. It
// never carries a file system path of the service.
type Problem struct {
	// Field is the location inside the settings, such as vault.admin_timeout.
	Field   string `json:"field,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}
