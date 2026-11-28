package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/cliport"
)

// `status --short` lives in a shell prompt: it answers within its timeout,
// falls back to the last answer moved forward by the time since, marks that
// answer as not fresh, says "offline" when it has nothing, and never fails.
func TestRemoteStatusShortForPrompts(t *testing.T) {
	var hang, refuse, forbid bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hang {
			time.Sleep(2 * time.Second)
		}
		if refuse {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if forbid {
			w.WriteHeader(http.StatusForbidden)
			return
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

	// A cached line older than its shelf life shows no numbers.
	now = now.Add(20 * time.Minute)
	if got := short(); got != "?" {
		t.Fatalf("stale cache line = %q", got)
	}

	// Another account (direnv switched the token): the cached line is not its.
	now = now.Add(-20 * time.Minute)
	rt.RemoteToken = "pt_other"
	if got := short(); got != "offline" {
		t.Fatalf("another account read the cache: %q", got)
	}
	rt.RemoteToken = "pt_x"
	now = now.Add(20 * time.Minute)

	// A refused token says so instead of hiding behind the cache.
	hang, refuse = false, true
	if got := short(); got != "auth?" {
		t.Fatalf("refused token line = %q", got)
	}
	if _, err := os.Stat(rt.StatusCachePath); !os.IsNotExist(err) {
		t.Fatalf("a refused token must drop the cache: %v", err)
	}
	refuse, forbid = false, true
	if got := short(); got != "access?" {
		t.Fatalf("token without a seat line = %q", got)
	}
	forbid = false
	_ = short()

	hang = false
	if err := RunWithRuntime([]string{"stop", "7"}, rt); err != nil {
		t.Fatalf("stop: %v", err)
	}
	hang = true
	if got := short(); got != "offline" {
		t.Fatalf("after stop the cache must be gone, got %q", got)
	}
}

// Without a server, --short reads the local database and keeps the prompt
// contract: one line, and "offline" instead of an error when it cannot.
func TestLocalStatusShortKeepsThePromptContract(t *testing.T) {
	var out bytes.Buffer
	rt := NewRuntime(nil, &out, &out)
	rt.ServiceLoader = func(context.Context) (*cliport.Services, io.Closer, error) {
		return nil, nil, errors.New("database is down")
	}
	if err := RunWithRuntime([]string{"status", "--short"}, rt); err != nil {
		t.Fatalf("local --short failed: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "offline" {
		t.Fatalf("local --short with no database = %q", got)
	}
}
