package web

import (
	"regexp"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAPITokensLifecycle(t *testing.T) {
	d, err := newTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if _, err := d.SQL().ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`); err != nil {
		t.Fatal(err)
	}
	raw, tok, err := d.CreateAPIToken(ctx, 1, "ext")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "pt_") {
		t.Fatalf("raw=%q", raw)
	}
	got, err := d.APITokenByRaw(ctx, raw)
	if err != nil || got.ID != tok.ID {
		t.Fatalf("lookup=%+v err=%v", got, err)
	}
	if _, err := d.APITokenByRaw(ctx, "pt_nope"); err == nil {
		t.Fatal("bad token accepted")
	}
	if err := d.DeleteAPIToken(ctx, 1, tok.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.APITokenByRaw(ctx, raw); err == nil {
		t.Fatal("deleted token still valid")
	}
}

func TestIntegrationsCRUD(t *testing.T) {
	d, err := newTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	d.SQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`)
	d.SQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)
	it, err := d.CreateIntegration(ctx, 1, "github", "acme/api", "ghp_x", `{"target":"acme/api"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateIntegration(ctx, 1, "github", "acme/api", "ghp_x", "{}"); err == nil {
		t.Fatal("duplicate accepted")
	}
	if _, err := d.UpsertExternalTask(ctx, it.ID, "gh-1", "Fix login", "https://x", "open"); err != nil {
		t.Fatal(err)
	}
	// upsert refresh
	if _, err := d.UpsertExternalTask(ctx, it.ID, "gh-1", "Fix login v2", "https://x", "closed"); err != nil {
		t.Fatal(err)
	}
	ts, err := d.ListExternalTasks(ctx, it.ID)
	if err != nil || len(ts) != 1 || ts[0].Title != "Fix login v2" {
		t.Fatalf("tasks=%+v err=%v", ts, err)
	}
	if err := d.DeleteIntegration(ctx, 1, it.ID); err != nil {
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
func fakeProviders(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)
	old := extClient.Transport
	extClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.Header.Set("X-Orig-Host", r.URL.Host)
		r.URL.Scheme, r.URL.Host = target.Scheme, target.Host
		return http.DefaultTransport.RoundTrip(r)
	})
	t.Cleanup(func() { extClient.Transport = old })
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestJiraSearchJQL(t *testing.T) {
	var gotPath, gotJQL, gotHost string
	fakeProviders(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotJQL, gotHost = r.URL.Path, r.URL.Query().Get("jql"), r.Header.Get("X-Orig-Host")
		fmt.Fprint(w, `{"issues":[{"key":"PROJ-1","fields":{"summary":"Fix","status":{"name":"To Do"}}}]}`)
	})
	if _, err := fetchJiraIssues("a:b", "PROJ"); err == nil {
		t.Fatal("no site anywhere must be an error")
	}
	items, err := fetchJiraIssues("a:b", "https://acme.atlassian.net PROJ")
	if err != nil || len(items) != 1 || items[0].URL != "https://acme.atlassian.net/browse/PROJ-1" {
		t.Fatalf("items %+v err %v", items, err)
	}
	if gotHost != "acme.atlassian.net" || gotPath != "/rest/api/3/search/jql" || !strings.HasPrefix(gotJQL, "project = PROJ AND") {
		t.Errorf("host %q path %q jql %q", gotHost, gotPath, gotJQL)
	}
	for _, raw := range []string{"project=PROJ", "assignee in (currentUser())"} {
		fetchJiraIssues("a:b", "https://acme.atlassian.net "+raw)
		if gotJQL != raw {
			t.Errorf("raw JQL %q was rewritten to %q", raw, gotJQL)
		}
	}
}

// GitHub's issues API returns pull requests too, and issue numbers repeat
// across repos.
func TestGitHubSkipsPRsAndKeysByRepo(t *testing.T) {
	fakeProviders(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"number":1,"title":"a","repository":{"full_name":"o/x"}},
			{"number":1,"title":"b","repository":{"full_name":"o/y"}},
			{"number":2,"title":"pr","pull_request":{},"repository":{"full_name":"o/x"}}]`)
	})
	items, err := fetchGitHubIssues("t", "")
	if err != nil || len(items) != 2 || items[0].ID == items[1].ID {
		t.Fatalf("items %+v err %v", items, err)
	}
}

func TestTodoistLinkWithoutURLField(t *testing.T) {
	fakeProviders(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"results":[{"id":"99","content":"call"}]}`)
	})
	items, err := fetchTodoistTasks("t", "")
	if err != nil || len(items) != 1 || items[0].URL != "https://app.todoist.com/app/task/99" {
		t.Fatalf("items %+v err %v", items, err)
	}
}

func TestNotionDatabaseIDFormat(t *testing.T) {
	fakeProviders(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) })
	_, err := fetchNotionTasks("secret", "not-a-uuid")
	if err == nil || !strings.Contains(err.Error(), "32 hex") {
		t.Fatalf("err=%v", err)
	}
	// well-formed id should reach the network layer (we only check parse)
	// by asserting the error is NOT a format error.
	_, err = fetchNotionTasks("secret", "00000000000000000000000000000000")
	if err != nil && strings.Contains(err.Error(), "32 hex") {
		t.Fatalf("id parsed but rejected: %v", err)
	}
}

func TestNewProviderGuards(t *testing.T) {
	fakeProviders(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) })
	cases := []struct {
		fn   func(string, string) ([]extItem, error)
		sec  string
		want string
	}{
		{fetchAsanaTasks, "tok", "project gid"},
		{fetchGitLabIssues, "tok", "group/project"},
		{fetchClickUpTasks, "tok", "list id"},
		{fetchGitLabIssues, "tok", "PARATRACK_GITLAB_SITE"}, // has default, so project error
	}
	// asana / gitlab / clickup empty-target errors
	if _, err := fetchAsanaTasks("t", ""); err == nil || !strings.Contains(err.Error(), "project gid") {
		t.Fatalf("asana: %v", err)
	}
	if _, err := fetchGitLabIssues("t", ""); err == nil || !strings.Contains(err.Error(), "group/project") {
		t.Fatalf("gitlab: %v", err)
	}
	if _, err := fetchClickUpTasks("t", ""); err == nil || !strings.Contains(err.Error(), "list id") {
		t.Fatalf("clickup: %v", err)
	}
	// todoist accepts empty target (all tasks) — just ensure it builds a request
	// (network may 401, that's fine).
	_, err := fetchTodoistTasks("t", "")
	if err != nil && strings.Contains(err.Error(), "required") {
		t.Fatalf("todoist should not require target: %v", err)
	}
	_ = cases
}
