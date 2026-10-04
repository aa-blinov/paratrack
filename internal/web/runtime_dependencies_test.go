package web

import (
	"context"
	"testing"

	"github.com/aa-blinov/paratrack/internal/mail"
	"github.com/aa-blinov/paratrack/internal/oidcport"
)

type nilOIDCProvider struct{}

func (*nilOIDCProvider) AuthorizationURL(context.Context, oidcport.AuthorizationRequest) (string, error) {
	return "", nil
}

func (*nilOIDCProvider) Authenticate(context.Context, oidcport.AuthenticationRequest) (oidcport.Identity, error) {
	return oidcport.Identity{}, nil
}

func (*nilOIDCProvider) CloseIdleConnections() { panic("typed nil OIDC provider was closed") }

func TestNewRequiresMailWorkerForAvailableSender(t *testing.T) {
	server, _ := newTestServer(t)
	runtime := server.runtime
	runtime.Mailer = mail.SMTPSender{Host: "smtp.example.test:587"}
	runtime.MailQueueWorker = nil

	_, err := New(server.services, server.addr, server.config, runtime)
	if err == nil || err.Error() != "incomplete HTTP runtime dependencies: invoice mail worker" {
		t.Fatalf("New() error = %v, want missing invoice mail worker", err)
	}
}

func TestRuntimeDependenciesCloseIgnoresTypedNilOIDC(t *testing.T) {
	var provider *nilOIDCProvider
	RuntimeDependencies{OIDC: provider}.Close()
}
