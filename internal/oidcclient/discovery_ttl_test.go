package oidcclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/oidcport"
)

// The document is read once and trusted for the TTL; after the TTL the
// provider's endpoint change reaches the client without a restart.
func TestDiscoveryIsRefreshedAfterItsTTL(t *testing.T) {
	var reads atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		issuer := server.URL
		auth := issuer + "/auth-first"
		if reads.Add(1) > 1 {
			auth = issuer + "/auth-second"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issuer":"` + issuer + `","authorization_endpoint":"` + auth + `","token_endpoint":"` + issuer + `/token","userinfo_endpoint":"` + issuer + `/userinfo","jwks_uri":"` + issuer + `/jwks","id_token_signing_alg_values_supported":["RS256"]}`))
	}))
	defer server.Close()

	client, err := New(server.Client(), Config{Issuer: server.URL, ClientID: "id"})
	if err != nil {
		t.Fatal(err)
	}
	fakeNow := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	client.now = func() time.Time { return fakeNow }
	request := oidcport.AuthorizationRequest{RedirectURI: server.URL + "/callback"}

	url, err := client.AuthorizationURL(context.Background(), request)
	if err != nil {
		t.Fatalf("first authorization URL: %v", err)
	}
	if !strings.Contains(url, "/auth-first") || reads.Load() != 1 {
		t.Fatalf("first call = %q with %d reads, want the first endpoint read once", url, reads.Load())
	}

	// Within the TTL the cached document answers; the provider's change
	// stays invisible, and that is the point of a cache.
	url, err = client.AuthorizationURL(context.Background(), request)
	if err != nil {
		t.Fatalf("second authorization URL: %v", err)
	}
	if !strings.Contains(url, "/auth-first") || reads.Load() != 1 {
		t.Fatalf("within the TTL the document was read again (%d reads)", reads.Load())
	}

	// After the TTL the new endpoints are picked up without a restart.
	fakeNow = fakeNow.Add(discoveryTTL + time.Minute)
	url, err = client.AuthorizationURL(context.Background(), request)
	if err != nil {
		t.Fatalf("refreshed authorization URL: %v", err)
	}
	if !strings.Contains(url, "/auth-second") || reads.Load() != 2 {
		t.Fatalf("after the TTL the client still serves the old endpoint: %q (%d reads)", url, reads.Load())
	}
}
