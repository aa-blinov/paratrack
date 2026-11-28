package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRemoteCLITokenFlow(t *testing.T) {
	var seenAuth string
	var startedActivity string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/start":
			_ = r.ParseForm()
			startedActivity = r.FormValue("activity")
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/api/active":
			_, _ = io.WriteString(w, `{"Items":[{"ID":7,"ActivityName":"API work","ProjectName":"Core","Duration":"12m","Paused":false,"Note":""}]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/sessions/7/stop":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	var out bytes.Buffer
	rt := NewRuntime(nil, &out, &out)
	rt.RemoteURL = srv.URL
	rt.RemoteToken = "pt_test_token"
	rt.HTTPClient = srv.Client()

	if err := RunWithRuntime([]string{"start", "API work"}, rt); err != nil {
		t.Fatalf("remote start: %v", err)
	}
	if seenAuth != "Bearer pt_test_token" || startedActivity != "API work" {
		t.Fatalf("unexpected request: auth=%q activity=%q", seenAuth, startedActivity)
	}

	out.Reset()
	if err := RunWithRuntime([]string{"status"}, rt); err != nil {
		t.Fatalf("remote status: %v", err)
	}
	if !strings.Contains(out.String(), "API work") || !strings.Contains(out.String(), "#7") {
		t.Fatalf("unexpected status output: %q", out.String())
	}

	out.Reset()
	if err := RunWithRuntime([]string{"stop", "API work"}, rt); err != nil {
		t.Fatalf("remote stop: %v", err)
	}
	if !strings.Contains(out.String(), "stopped") {
		t.Fatalf("unexpected stop output: %q", out.String())
	}
}
