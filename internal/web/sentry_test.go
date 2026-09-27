package web

import (
	"context"
	"io"
	"time"

	"github.com/aa-blinov/paratrack/internal/db"
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

// A failing endpoint is retried; V2 signs timestamp.body.
func TestWebhookRetriesAndSignsTimestamp(t *testing.T) {
	t.Setenv("PARATRACK_WEBHOOK_ALLOW_PRIVATE", "1")
	old := webhookBackoff
	webhookBackoff = []time.Duration{0, 0, 0}
	defer func() { webhookBackoff = old }()
	calls := 0
	var sigOK bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		ts := r.Header.Get("X-Paratrack-Timestamp")
		sigOK = r.Header.Get("X-Paratrack-Signature-V2") == signPayload("s3cret", append([]byte(ts+"."), body...))
		if calls < 3 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	d.SQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`)
	d.SQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)
	h, err := d.CreateWebhook(ctx, 1, srv.URL, "s3cret", "*")
	if err != nil {
		t.Fatal(err)
	}
	(&Server{db: d}).deliverWebhook(h, "session.stopped", []byte(`{"x":1}`))
	if calls != 3 || !sigOK {
		t.Fatalf("calls %d sigOK %v, want 3 attempts and a valid V2 signature", calls, sigOK)
	}
	if ds, _ := d.ListWebhookDeliveries(ctx, h.ID); len(ds) != 3 || ds[0].Status != 200 {
		t.Errorf("delivery log %+v", ds)
	}
}
