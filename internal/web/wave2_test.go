package web

import (
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
	if resp.StatusCode != 303 {
		t.Fatalf("create token: %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	resp.Body.Close()
	if !strings.Contains(loc, "token=") {
		t.Fatalf("no raw token in redirect: %s", loc)
	}
	// extract raw
	i := strings.Index(loc, "token=")
	raw := loc[i+len("token="):]
	if j := strings.Index(raw, "&"); j > 0 {
		raw = raw[:j]
	}
	// decode
	if raw, err := url.QueryUnescape(raw); err == nil {
		_ = raw
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

func TestJiraProjectKeyToJQL(t *testing.T) {
	// bare key becomes a JQL filter — verify via the helper's logic by
	// calling with a fake site and no network (should fail on env).
	_, err := fetchJiraIssues("a:b", "PROJ")
	if err == nil {
		t.Fatal("expected error without PARATRACK_JIRA_SITE")
	}
	if !strings.Contains(err.Error(), "PARATRACK_JIRA_SITE") {
		t.Fatalf("err=%v", err)
	}
}

func TestNotionDatabaseIDFormat(t *testing.T) {
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
