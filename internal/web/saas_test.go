package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/mail"
)

func TestCSRFRejectedWithoutToken(t *testing.T) {
	e := newAPIEnv(t)
	// Bypass the helper's auto-CSRF by talking to the server directly.
	req, _ := http.NewRequest("POST", e.ts.URL+"/api/login", strings.NewReader("email=a@b.c&password=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST without CSRF: status %d, want 403", resp.StatusCode)
	}
}

func TestSecurityHeadersPresent(t *testing.T) {
	e := newAPIEnv(t)
	resp := e.do("GET", "/login", nil, nil)
	defer resp.Body.Close()
	for h, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
	} {
		if got := resp.Header.Get(h); got != want {
			t.Errorf("%s = %q, want %q", h, got, want)
		}
	}
	if resp.Header.Get("Content-Security-Policy") == "" {
		t.Error("missing Content-Security-Policy")
	}
}

func TestPasswordResetFlow(t *testing.T) {
	e := newAPIEnv(t)
	// Capture the mail the log sender writes by swapping in a spy.
	spy := &spyMail{}
	e.srv.mailer = spy

	e.register("reset@x.test")

	// Ask for a reset link AS A LOGGED-OUT CLIENT — forgot must be
	// public (the user is locked out). Fresh jar, no session cookie.
	anon := &apiEnv{t: t, ts: e.ts, srv: e.srv, jar: map[string]string{}}
	resp := anon.do("POST", "/api/password/forgot", url.Values{"email": {"reset@x.test"}}, nil)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("forgot (anon): status %d, want 303 (route must be public)", resp.StatusCode)
	}
	resp.Body.Close()
	if len(spy.sent) == 0 {
		t.Fatal("no mail sent for known address")
	}
	body := spy.sent[0].body
	// Extract ?token=…
	i := strings.Index(body, "token=")
	if i < 0 {
		t.Fatalf("no token in mail body: %q", body)
	}
	token := strings.TrimSpace(strings.SplitN(body[i+len("token="):], "\n", 2)[0])

	// Consume the token (also public).
	resp = anon.do("POST", "/api/password/reset", url.Values{
		"token": {token}, "new_password": {"brandnewpw1"},
	}, nil)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("reset (anon): status %d, want 303", resp.StatusCode)
	}
	resp.Body.Close()

	// Old password no longer works, new one does.
	resp = e.do("POST", "/api/login", url.Values{"email": {"reset@x.test"}, "password": {"longenoughpw"}}, nil)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("old pw login: %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "login") {
		t.Errorf("old pw should bounce to /login, got %q", loc)
	}
	resp.Body.Close()

	resp = e.do("POST", "/api/login", url.Values{"email": {"reset@x.test"}, "password": {"brandnewpw1"}}, nil)
	if loc := resp.Header.Get("Location"); loc != "/" {
		t.Errorf("new pw login: Location = %q, want /", loc)
	}
	resp.Body.Close()

	// Token is single-use.
	resp = e.do("POST", "/api/password/reset", url.Values{
		"token": {token}, "new_password": {"againpw1234"},
	}, nil)
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "reset-password") {
		t.Errorf("reused token should bounce back, got %q", loc)
	}
	resp.Body.Close()
}

func TestRateLimitLogin(t *testing.T) {
	e := newAPIEnv(t)
	code := 0
	for i := 0; i < 12; i++ {
		resp := e.do("POST", "/api/login", url.Values{"email": {"x@y.z"}, "password": {"wrongwrong"}}, nil)
		code = resp.StatusCode
		resp.Body.Close()
	}
	if code != http.StatusTooManyRequests {
		t.Fatalf("after 12 attempts status = %d, want 429", code)
	}
}

func TestBackfillCreatesClosedSession(t *testing.T) {
	e := newAPIEnv(t)
	e.register("backfill@x.test")
	start := time.Now().Add(-3 * time.Hour).Format("15:04")
	end := time.Now().Add(-1 * time.Hour).Format("15:04")
	resp := e.do("POST", "/api/sessions/backfill", url.Values{
		"activity": {"reading"},
		"start":    {"yesterday " + start},
		"end":      {"yesterday " + end},
		"note":     {"backfilled"},
	}, map[string]string{"HX-Request": "true"})
	if resp.StatusCode != 200 {
		t.Fatalf("backfill: %d %s", resp.StatusCode, readBody(t, resp))
	}
	resp.Body.Close()

	resp = e.do("GET", "/api/reports.csv", nil, nil)
	csv := readBody(t, resp)
	if !strings.Contains(csv, "reading") || !strings.Contains(csv, "backfilled") {
		t.Fatalf("CSV missing backfilled session:\n%s", csv)
	}
}

type spyMail struct{ sent []struct{ to, subject, body string } }

func (s *spyMail) Send(to, subject, body string) error {
	s.sent = append(s.sent, struct{ to, subject, body string }{to, subject, body})
	return nil
}

var _ mail.Sender = (*spyMail)(nil)

// keep httptest import used when tests grow
var _ = httptest.NewRecorder
