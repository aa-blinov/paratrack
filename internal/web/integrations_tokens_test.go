package web

import (
	"github.com/aa-blinov/paratrack/internal/integrationport"

	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/netclients"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestAPITokensLifecycle(t *testing.T) {
	d, err := newTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`); err != nil {
		t.Fatal(err)
	}
	ctx = requestctx.WithActor(ctx, 1)
	raw, tok, err := d.CreateAPIToken(ctx, appmodel.APITokenCreateRequest{UserID: 1, CallerID: 1, Name: "ext"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "pt_") {
		t.Fatalf("raw=%q", raw)
	}
	got, err := d.APITokenByRaw(ctx, appmodel.APITokenLookupRequest{Raw: raw})
	if err != nil || got.ID != tok.ID {
		t.Fatalf("lookup=%+v err=%v", got, err)
	}
	if _, err := d.APITokenByRaw(ctx, appmodel.APITokenLookupRequest{Raw: "pt_nope"}); err == nil {
		t.Fatal("bad token accepted")
	}
	if err := d.DeleteAPIToken(ctx, appmodel.APITokenDeleteRequest{UserID: 1, CallerID: 1, TokenID: tok.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.APITokenByRaw(ctx, appmodel.APITokenLookupRequest{Raw: raw}); err == nil {
		t.Fatal("deleted token still valid")
	}
}

func TestIntegrationsCRUD(t *testing.T) {
	d, err := newTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	d.TestSQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`)
	d.TestSQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)
	d.TestSQL().ExecContext(ctx, `INSERT INTO memberships (team_id, user_id, role) VALUES (1,1,'owner')`)
	it, err := d.CreateIntegration(ctx, appmodel.IntegrationCreateRequest{
		TeamID: 1, CallerID: 1, Provider: "github", Name: "acme/api", Secret: "ghp_x", Config: appmodel.IntegrationConfig{Target: "acme/api"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateIntegration(ctx, appmodel.IntegrationCreateRequest{
		TeamID: 1, CallerID: 1, Provider: "github", Name: "acme/api", Secret: "ghp_x", Config: appmodel.IntegrationConfig{},
	}); err == nil {
		t.Fatal("duplicate accepted")
	}
	syncOne, err := d.BeginIntegrationSync(ctx, appmodel.IntegrationSyncStartRequest{TeamID: 1, IntegrationID: it.ID, CallerID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SyncExternalTasks(ctx, appmodel.IntegrationTaskSyncRequest{TeamID: 1, IntegrationID: it.ID, CallerID: 1, Generation: syncOne.Generation, Tasks: []integrationport.ProviderTask{{ExternalID: "gh-1", Title: "Fix login", URL: "https://x", Status: "open"}}}); err != nil {
		t.Fatal(err)
	}
	// A second provider snapshot refreshes the existing task atomically.
	syncTwo, err := d.BeginIntegrationSync(ctx, appmodel.IntegrationSyncStartRequest{TeamID: 1, IntegrationID: it.ID, CallerID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SyncExternalTasks(ctx, appmodel.IntegrationTaskSyncRequest{TeamID: 1, IntegrationID: it.ID, CallerID: 1, Generation: syncTwo.Generation, Tasks: []integrationport.ProviderTask{{ExternalID: "gh-1", Title: "Fix login v2", URL: "https://x", Status: "closed"}}}); err != nil {
		t.Fatal(err)
	}
	ts, err := d.ListExternalTasks(ctx, 1, it.ID)
	if err != nil || len(ts) != 1 || ts[0].Title != "Fix login v2" {
		t.Fatalf("tasks=%+v err=%v", ts, err)
	}
	if err := d.DeleteIntegration(ctx, appmodel.IntegrationMutationRequest{TeamID: 1, IntegrationID: it.ID, CallerID: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestBearerTokenAuth(t *testing.T) {
	e := newAPIEnv(t)
	e.register("bearer@x.test")
	// mint a token via the form endpoint
	resp := e.do("POST", "/api/tokens", url.Values{"name": {"ext"}}, nil)
	// The raw token is in the page itself, never in a redirect URL.
	if resp.StatusCode != 200 || resp.Header.Get("Location") != "" {
		t.Fatalf("create token: %d loc %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	page := readBody(t, resp)
	raw := regexp.MustCompile(`pt_[A-Za-z0-9_-]+`).FindString(page)
	if raw == "" {
		t.Fatal("raw token not shown on the page")
	}
	// call /api/me with Bearer on a fresh env sharing the server
	// (use the same e but clear cookies then send Bearer)
	saved := e.jar
	e.jar = map[string]string{"paratrack_csrf": saved["paratrack_csrf"]}
	resp = e.do("GET", "/api/me", nil, map[string]string{
		"Authorization": "Bearer " + mustUnescape(t, raw),
	})
	if resp.StatusCode != 200 {
		t.Fatalf("bearer /api/me: %d %s", resp.StatusCode, readBody(t, resp))
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "bearer@x.test") {
		t.Fatalf("me body=%s", body)
	}
	e.jar = saved
}

func TestAPITokenCreateRejectsInvalidExpiry(t *testing.T) {
	e := newAPIEnv(t)
	e.register("token-expiry@x.test")

	for _, values := range []url.Values{
		{"name": {"bad-text"}, "expires_days": {"abc"}},
		{"name": {"bad-range"}, "expires_days": {"31"}},
		{"name": {"duplicate"}, "expires_days": {"30", "365"}},
	} {
		resp := e.do("POST", "/api/tokens", values, nil)
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("invalid expiry %v: status %d", values["expires_days"], resp.StatusCode)
		}
		resp.Body.Close()
	}

	var count int
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `
		SELECT count(*) FROM api_tokens t JOIN users u ON u.id = t.user_id
		WHERE u.email = 'token-expiry@x.test'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("invalid expiry created %d API tokens", count)
	}
}

func mustUnescape(t *testing.T, s string) string {
	t.Helper()
	out, err := url.QueryUnescape(s)
	if err != nil {
		return s
	}
	return out
}

// fakeProviders answers every outbound API call from h, whatever the
// host, so importers run end to end without the network.
func fakeProviders(t *testing.T, h http.HandlerFunc) *http.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)
	client := netclients.External()
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.Header.Set("X-Orig-Host", r.URL.Host)
		r.URL.Scheme, r.URL.Host = target.Scheme, target.Host
		return http.DefaultTransport.RoundTrip(r)
	})
	return client
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAPITokenScopes(t *testing.T) {
	e := newAPIEnv(t)
	e.register("scopes@x.test")
	d := e.db
	ctx := t.Context()
	var uid int64
	d.TestSQL().QueryRowContext(ctx, `SELECT id FROM users WHERE email = 'scopes@x.test'`).Scan(&uid)
	ctx = requestctx.WithActor(ctx, uid)
	call := func(method, path, tok string) int {
		req, _ := http.NewRequest(method, e.ts.URL+path, strings.NewReader("activity=x"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	ro, _, _ := d.CreateAPIToken(ctx, appmodel.APITokenCreateRequest{UserID: uid, CallerID: uid, Name: "ro", Options: db.TokenOptions{ReadOnly: true}})
	if c := call("GET", "/api/me", ro); c != 200 {
		t.Errorf("read-only GET: %d", c)
	}
	if c := call("POST", "/api/start", ro); c != 403 {
		t.Errorf("read-only POST: %d, want 403", c)
	}
	// A Bearer request is CSRF-exempt, so it must keep the Bearer principal
	// even when the same browser request also carries a writable session cookie.
	mixed := e.do("POST", "/api/start", url.Values{"activity": {"cookie-bearer-mixed"}}, map[string]string{
		"Authorization": "Bearer " + ro,
	})
	if mixed.StatusCode != http.StatusForbidden {
		t.Errorf("read-only Bearer with session cookie: %d, want 403", mixed.StatusCode)
	}
	mixed.Body.Close()
	past := time.Now().Add(-time.Hour)
	old, _, _ := d.CreateAPIToken(ctx, appmodel.APITokenCreateRequest{UserID: uid, CallerID: uid, Name: "old", Options: db.TokenOptions{ExpiresAt: &past}})
	if c := call("GET", "/api/me", old); c != 401 {
		t.Errorf("expired token: %d, want 401", c)
	}
	rw, _, _ := d.CreateAPIToken(ctx, appmodel.APITokenCreateRequest{UserID: uid, CallerID: uid, Name: "rw"})
	if c := call("POST", "/api/start", rw); c != 200 {
		t.Errorf("read-write POST: %d", c)
	}
}
