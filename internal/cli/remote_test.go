package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteCLITokenFlow(t *testing.T) {
	var seenAuth string
	var startedActivity string
	var startedProject string
	var addedTag string
	var backfillActivity string
	var focusedTarget string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects":
			_, _ = io.WriteString(w, `{"projects":[{"id":42,"name":"Core","slug":"core"}]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/start":
			_ = r.ParseForm()
			startedActivity = r.FormValue("activity")
			startedProject = r.FormValue("project_id")
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/sessions/7/tags":
			_ = r.ParseForm()
			addedTag = r.FormValue("name")
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/api/active":
			_, _ = io.WriteString(w, `{"Items":[{"ID":7,"ActivityName":"API work","ProjectName":"Core","Duration":"12m","Paused":false,"Note":""}]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/sessions/7/stop":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/focus/"):
			focusedTarget = strings.TrimPrefix(r.URL.Path, "/api/focus/")
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/sessions/backfill":
			_ = r.ParseForm()
			backfillActivity = r.FormValue("activity")
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sessions":
			_, _ = io.WriteString(w, `{"sessions":[{"id":9,"activity":"API work","start":"2026-11-28T10:00:00Z","end":"2026-11-28T11:30:00Z","seconds":5400,"note":"PR"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/reports/summary":
			_, _ = io.WriteString(w, `{"from":"2026-11-28","to":"2026-11-28","total_seconds":5400,"by_activity":{"Beta":1800,"Alpha":3600}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	var out bytes.Buffer
	rt := NewRuntime(nil, &out, &out)
	rt.RemoteConfigPath = filepath.Join(t.TempDir(), "config.json")
	rt.HTTPClient = srv.Client()

	if err := RunWithRuntime([]string{"auth", "login", "--url", srv.URL, "--token", "pt_test_token"}, rt); err != nil {
		t.Fatalf("auth login: %v", err)
	}
	if rt.RemoteURL != srv.URL || rt.RemoteToken != "pt_test_token" {
		t.Fatalf("auth login did not populate runtime")
	}

	out.Reset()
	if err := RunWithRuntime([]string{"start", "API work", "--project", "core", "--tag", "backend"}, rt); err != nil {
		t.Fatalf("remote start: %v", err)
	}
	if seenAuth != "Bearer pt_test_token" || startedActivity != "API work" || startedProject != "42" || addedTag != "backend" {
		t.Fatalf("unexpected request: auth=%q activity=%q project=%q tag=%q", seenAuth, startedActivity, startedProject, addedTag)
	}

	out.Reset()
	if err := RunWithRuntime([]string{"status", "--short"}, rt); err != nil {
		t.Fatalf("remote status --short: %v", err)
	}
	if strings.TrimSpace(out.String()) != "API work 12m" {
		t.Fatalf("unexpected short status output: %q", out.String())
	}

	out.Reset()
	if err := RunWithRuntime([]string{"focus", "Code review"}, rt); err != nil {
		t.Fatalf("remote focus by name: %v", err)
	}
	if focusedTarget != "Code review" {
		t.Fatalf("expected focus target 'Code review', got %q", focusedTarget)
	}

	out.Reset()
	if err := RunWithRuntime([]string{"add", "--activity", "API work", "--start", "2 hours ago", "--duration", "1h", "--project", "core"}, rt); err != nil {
		t.Fatalf("remote add: %v", err)
	}
	if backfillActivity != "API work" {
		t.Fatalf("unexpected backfill activity: %q", backfillActivity)
	}

	out.Reset()
	if err := RunWithRuntime([]string{"log", "--period", "today"}, rt); err != nil {
		t.Fatalf("remote log: %v", err)
	}
	if !strings.Contains(out.String(), "API work") || !strings.Contains(out.String(), "1h 30m") {
		t.Fatalf("unexpected log output: %q", out.String())
	}

	out.Reset()
	if err := RunWithRuntime([]string{"stats"}, rt); err != nil {
		t.Fatalf("remote stats: %v", err)
	}
	if idxA, idxB := strings.Index(out.String(), "Alpha"), strings.Index(out.String(), "Beta"); idxA < 0 || idxB < 0 || idxA > idxB {
		t.Fatalf("stats should be sorted descending by duration: %q", out.String())
	}

	if err := RunWithRuntime([]string{"project", "list"}, rt); err == nil || !strings.Contains(err.Error(), "not available in remote CLI mode") {
		t.Fatalf("expected remote-mode refusal for unsupported command, got %v", err)
	}

	out.Reset()
	if err := RunWithRuntime([]string{"stop", "API work"}, rt); err != nil {
		t.Fatalf("remote stop: %v", err)
	}
	if !strings.Contains(out.String(), "stopped") {
		t.Fatalf("unexpected stop output: %q", out.String())
	}
}
