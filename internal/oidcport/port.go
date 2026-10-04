// Package oidcport defines the outbound OIDC capability used by the web adapter.
package oidcport

import "context"

type AuthorizationRequest struct {
	RedirectURI string
	State       string
	Nonce       string
	Challenge   string
}

type AuthenticationRequest struct {
	Code        string
	Verifier    string
	Nonce       string
	RedirectURI string
}

type Identity struct {
	Subject       string
	Email         string
	Name          string
	EmailVerified bool
}

type Provider interface {
	AuthorizationURL(context.Context, AuthorizationRequest) (string, error)
	Authenticate(context.Context, AuthenticationRequest) (Identity, error)
	CloseIdleConnections()
}
