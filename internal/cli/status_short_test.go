package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// `status --short` lives in a shell prompt: it answers within its timeout,
// falls back to the last answer moved forward by the time since, marks that
// answer as not fresh, says "offline" when it has nothing, and never fails.
func TestRemoteStatusShortForPrompts(t *testing.T) {
	var hang bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hang {
			time.Sleep(2 * time.Second)
		}
		switch {
		case r.URL.Path == "/api/active":
			_, _ = io.WriteString(w, `{"Items":[{"ID":7,"ActivityName":"API work","DurationSecs":3900,"Paused":false}]}`)
		case r.URL.Path == "/api/sessions/7/stop":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	rt := NewRuntime(nil, &out, &out)
	rt.Now = func() time.Time { return now }
	rt.RemoteURL, rt.RemoteToken = srv.URL, "pt_x"
	rt.HTTPClient = srv.Client()
	rt.StatusCachePath = filepath.Join(t.TempDir(), "status.json")

	short := func() string {
		t.Helper()
		out.Reset()
		if err := RunWithRuntime([]string{"status", "--short"}, rt); err != nil {
			t.Fatalf("status --short: %v", err)
		}
		return strings.TrimSpace(out.String())
	}
	if got := short(); got != "API work 1h05m" {
		t.Fatalf("fresh line = %q", got)
	}

	hang = true
	now = now.Add(10 * time.Minute)
	began := time.Now()
	if got := short(); got != "API work 1h15m ?" {
		t.Fatalf("cached line = %q", got)
	}
	if waited := time.Since(began); waited > time.Second {
		t.Fatalf("status --short waited %s for a hanging server", waited)
	}

	hang = false
	if err := RunWithRuntime([]string{"stop", "7"}, rt); err != nil {
		t.Fatalf("stop: %v", err)
	}
	hang = true
	if got := short(); got != "offline" {
		t.Fatalf("after stop the cache must be gone, got %q", got)
	}
}
