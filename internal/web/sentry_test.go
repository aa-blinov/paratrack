package web

import (
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/netclients"
	"github.com/getsentry/sentry-go"
)

func TestValidateSentryConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  SentryConfig
		wantErr bool
	}{
		{name: "disabled", config: SentryConfig{}},
		{name: "valid", config: SentryConfig{DSN: "https://public@example.com/42", TracesSampleRate: 0.25}},
		{name: "local http", config: SentryConfig{DSN: "http://public@localhost/42", Environment: "development"}},
		{name: "nan rate", config: SentryConfig{TracesSampleRate: math.NaN()}, wantErr: true},
		{name: "infinite rate", config: SentryConfig{TracesSampleRate: math.Inf(1)}, wantErr: true},
		{name: "rate above one", config: SentryConfig{TracesSampleRate: 1.1}, wantErr: true},
		{name: "dsn without key", config: SentryConfig{DSN: "https://example.com/42"}, wantErr: true},
		{name: "dsn without project", config: SentryConfig{DSN: "https://public@example.com"}, wantErr: true},
		{name: "production http", config: SentryConfig{DSN: "http://public@example.com/42"}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateSentryConfig(test.config)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateSentryConfig() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestScrubSentryRequestRemovesQueryCredentials(t *testing.T) {
	event := &sentry.Event{Request: &sentry.Request{
		URL:         "https://paratrack.example/reset-password?token=secret#form",
		Data:        "password=secret",
		QueryString: "token=secret",
		Cookies:     "session=secret",
		Env:         map[string]string{"REMOTE_ADDR": "192.0.2.1"},
		Headers: map[string]string{
			"Referer":       "https://paratrack.example/reset-password?token=secret",
			"Authorization": "Bearer secret", "Cookie": "session=secret",
			"Set-Cookie": "session=secret", "X-Api-Key": "secret-key",
			"Host": "paratrack.example",
		},
	}}
	got := scrubSentryRequest(event, nil)
	if got.Request.URL != "https://paratrack.example/reset-password" || got.Request.QueryString != "" {
		t.Fatalf("Sentry request retained query credentials: %+v", got.Request)
	}
	if got.Request.Data != "" || got.Request.Cookies != "" || got.Request.Env != nil {
		t.Fatalf("Sentry request retained private context: %+v", got.Request)
	}
	for _, key := range []string{"Referer", "Authorization", "Cookie", "Set-Cookie", "X-Api-Key"} {
		if _, ok := got.Request.Headers[key]; ok {
			t.Fatalf("Sentry request retained %s: %+v", key, got.Request.Headers)
		}
	}
	if got.Request.Headers["Host"] != "paratrack.example" {
		t.Fatalf("Sentry request lost non-sensitive headers: %+v", got.Request.Headers)
	}
}

func TestSentryPrivacyPolicyDisablesAutomaticRequestCollection(t *testing.T) {
	policy := sentryPrivacyPolicy()
	if !policy.UserInfo.IsSet || policy.UserInfo.Value || policy.Cookies.Mode != sentry.CollectionOff ||
		policy.HTTPHeaders.Request.Mode != sentry.CollectionOff || policy.HTTPHeaders.Response.Mode != sentry.CollectionOff ||
		len(policy.HTTPBodies) != 0 || policy.QueryParams.Mode != sentry.CollectionOff {
		t.Fatalf("Sentry privacy policy is not opt-out: %+v", policy)
	}
}

func TestSentryTunnelClientOwnsTransport(t *testing.T) {
	first := newSentryTunnelClient()
	second := newSentryTunnelClient()
	if first.Transport == nil || second.Transport == nil || first.Transport == second.Transport {
		t.Fatal("Sentry tunnel clients must not share a transport")
	}
}

func TestSentryTunnelDoesNotBorrowCustomDefaultTransport(t *testing.T) {
	previous := http.DefaultTransport
	defer func() { http.DefaultTransport = previous }()
	http.DefaultTransport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("unexpected request")
	})

	client := newSentryTunnelClient()
	if client.Transport == http.DefaultTransport {
		t.Fatal("Sentry tunnel client borrowed the process default transport")
	}
	if _, ok := client.Transport.(*http.Transport); !ok {
		t.Fatalf("Sentry tunnel transport type = %T, want owned *http.Transport", client.Transport)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (roundTripper roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
}

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
	s.sentry = sentryState{}
	if c := post(`{"dsn":"https://k@o1.ingest.sentry.io/1"}`); c != 404 {
		t.Errorf("no DSN configured: %d, want 404", c)
	}
	dsn, _ := url.Parse("https://k@o1.ingest.sentry.io/1")
	s.sentry = sentryState{dsn: dsn, environment: "test"}
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
	if _, err := netclients.Webhook(false).Get(srv.URL); err == nil || !strings.Contains(err.Error(), "not a public address") {
		t.Fatalf("loopback webhook target allowed: %v", err)
	}
	resp, err := netclients.Webhook(true).Get(srv.URL)
	if err != nil {
		t.Fatalf("opt-in for private targets: %v", err)
	}
	resp.Body.Close()
}
