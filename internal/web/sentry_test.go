package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// statusWriter keeps the status and the cause of a 5xx, and nothing of a 2xx.
func TestStatusWriterKeepsFailureCause(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec}
	http.Error(sw, "db: connection refused", 500)
	if sw.status != 500 || !strings.Contains(string(sw.body), "connection refused") {
		t.Fatalf("status %d body %q", sw.status, sw.body)
	}
	ok := &statusWriter{ResponseWriter: httptest.NewRecorder()}
	ok.Write([]byte(strings.Repeat("x", 1000)))
	if ok.status != 200 || len(ok.body) != 0 {
		t.Fatalf("2xx must not be kept: status %d, %d bytes", ok.status, len(ok.body))
	}
}

// The tunnel relays only envelopes for our own DSN, and is off without one.
func TestSentryTunnelGuards(t *testing.T) {
	s := &Server{}
	post := func(body string) int {
		rec := httptest.NewRecorder()
		s.handleSentryTunnel(rec, httptest.NewRequest("POST", "/sentry-tunnel", strings.NewReader(body)))
		return rec.Code
	}
	sentryDSN = nil
	if c := post(`{"dsn":"https://k@o1.ingest.sentry.io/1"}`); c != 404 {
		t.Errorf("no DSN configured: %d, want 404", c)
	}
	sentryDSN, _ = url.Parse("https://k@o1.ingest.sentry.io/1")
	defer func() { sentryDSN = nil }()
	if c := post(`{"dsn":"https://k@evil.example/1"}` + "\n{}"); c != 400 {
		t.Errorf("foreign DSN: %d, want 400", c)
	}
	if c := post("not json"); c != 400 {
		t.Errorf("garbage: %d, want 400", c)
	}
}

// Webhooks never reach the server's own network.
func TestHookClientRefusesPrivateTargets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	if _, err := hookClient.Get(srv.URL); err == nil || !strings.Contains(err.Error(), "not a public address") {
		t.Fatalf("loopback webhook target allowed: %v", err)
	}
	t.Setenv("PARATRACK_WEBHOOK_ALLOW_PRIVATE", "1")
	resp, err := hookClient.Get(srv.URL)
	if err != nil {
		t.Fatalf("opt-in for private targets: %v", err)
	}
	resp.Body.Close()
}
