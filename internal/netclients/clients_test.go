package netclients

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aa-blinov/paratrack/internal/netpolicy"
)

func TestValidateProviderAddress(t *testing.T) {
	tests := []struct {
		name         string
		address      string
		allowPrivate bool
		wantErr      error
	}{
		{name: "public address", address: "8.8.8.8:443"},
		{name: "private address denied", address: "10.0.0.1:443", wantErr: netpolicy.ErrPrivateTarget},
		{name: "loopback denied", address: "127.0.0.1:443", wantErr: netpolicy.ErrPrivateTarget},
		{name: "private address explicitly allowed", address: "10.0.0.1:443", allowPrivate: true},
		{name: "hostname is not a dial address", address: "git.example.test:443", wantErr: netpolicy.ErrPrivateTarget},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateProviderAddress(test.address, test.allowPrivate)
			if test.wantErr == nil && err != nil {
				t.Fatalf("validateProviderAddress() error = %v", err)
			}
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("validateProviderAddress() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestExternalClientsOwnIndependentTransports(t *testing.T) {
	first := External()
	second := External()
	if first.Transport == nil || second.Transport == nil || first.Transport == second.Transport {
		t.Fatal("External clients must not share a transport")
	}
	if _, ok := first.Transport.(*http.Transport); !ok {
		t.Fatalf("External transport has type %T, want *http.Transport", first.Transport)
	}
}

func TestWebhookPrivateTargetOptInDoesNotChangePublicOnlyClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	response, err := Webhook(false).Get(server.URL)
	if response != nil {
		response.Body.Close()
	}
	if !errors.Is(err, netpolicy.ErrPrivateTarget) {
		t.Fatalf("Webhook(false) error = %v, want private-target rejection", err)
	}

	response, err = Webhook(true).Get(server.URL)
	if err != nil {
		t.Fatalf("Webhook(true) request: %v", err)
	}
	response.Body.Close()

	response, err = PublicOutbound().Get(server.URL)
	if response != nil {
		response.Body.Close()
	}
	if !errors.Is(err, netpolicy.ErrPrivateTarget) {
		t.Fatalf("PublicOutbound() error = %v, want private-target rejection", err)
	}
}
