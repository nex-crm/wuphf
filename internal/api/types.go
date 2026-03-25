package api

// RegisterRequest is the payload sent to the registration endpoint.
type RegisterRequest struct {
	Email       string `json:"email"`
	Name        string `json:"name,omitempty"`
	CompanyName string `json:"company_name,omitempty"`
}
