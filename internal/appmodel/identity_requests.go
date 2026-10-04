package appmodel

// UserIdentity is the credential-free account information safe to pass to
// transport adapters and request contexts.
type UserIdentity struct {
	ID    int64
	Email string
	Name  string
}

type RegistrationRequest struct {
	Email    string
	Password string `json:"-"` // Sensitive credential material.
	Name     string
	TeamName string
}

type SSOAuthenticationRequest struct {
	Email    string
	Name     string
	TeamName string
	Subject  string
}
