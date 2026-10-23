package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aa-blinov/paratrack/internal/integrationport"
)

// A GitHub token the provider refuses is named before the connection is saved; a token it
// accepts passes, and other providers are not asked (ADM-10).
func TestGitHubRefusedTokenIsNamedBeforeSaving(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" || r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client, err := NewWithConfigAndRetryWaits(server.Client(), nil, Config{GitHubAPI: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := client.CheckToken(ctx, integrationport.ProviderInput{Provider: "github", Secret: "bad"}); !errors.Is(err, integrationport.ErrTokenRejected) {
		t.Fatalf("a refused token answered %v, want ErrTokenRejected", err)
	}
	if err := client.CheckToken(ctx, integrationport.ProviderInput{Provider: "github", Secret: "good"}); err != nil {
		t.Fatalf("an accepted token answered %v", err)
	}
	if err := client.CheckToken(ctx, integrationport.ProviderInput{Provider: "jira", Secret: "bad"}); err != nil {
		t.Fatalf("another provider was checked here: %v", err)
	}
}
