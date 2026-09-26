package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/auth"
	dbpkg "github.com/aa-blinov/paratrack/internal/db"
)

// newTestServer builds a Server backed by a temp DB and seeds one
// user + personal team, returning the server, the seeded user, and a
// freshly minted session token so tests can hit authed paths.
func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	d, err := dbpkg.Open(t.TempDir() + "/web.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	srv, err := New(d, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("server: %v", err)
	}

	authSvc := auth.NewService(d)
	uid, _, err := authSvc.CreateUser(context.Background(),
		"alice@example.com", "longenough", "Alice")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	sess, err := authSvc.NewSession(context.Background(), uid)
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return srv, sess.Token
}

func TestPublicLoginPageAccessibleWithoutAuth(t *testing.T) {
	srv, _ := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/login", nil)
	srv.routes().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("GET /login: want 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Log in") {
		t.Errorf("body should contain 'Log in', got first 200 chars: %q",
			w.Body.String()[:min(200, len(w.Body.String()))])
	}
}

func TestProtectedPageRedirectsToLoginWhenUnauth(t *testing.T) {
	srv, _ := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/stats", nil)
	srv.routes().ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("GET /stats without cookie: want 303, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.HasPrefix(loc, "/login?next=") {
		t.Errorf("Location: want /login?next=…, got %q", loc)
	}
}

func TestProtectedPageAccessibleWithValidSession(t *testing.T) {
	srv, token := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/stats", nil)
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	srv.routes().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("GET /stats with cookie: want 200, got %d body=%q",
			w.Code, w.Body.String())
	}
}

func TestProtectedAPIReturns401JSONWhenUnauth(t *testing.T) {
	srv, _ := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/start", nil)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	csrfTok, csrfCk := seedCSRF(t, srv.routes())
	r.Header.Set(csrfHeaderName, csrfTok)
	r.AddCookie(csrfCk)
	srv.routes().ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("POST /api/start without cookie: want 401, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type: want JSON, got %q", ct)
	}
}

func TestLoginFlowEndToEnd(t *testing.T) {
	srv, _ := newTestServer(t)

	// Submit credentials to /api/login.
	w := httptest.NewRecorder()
	form := strings.NewReader("email=alice@example.com&password=longenough")
	r := httptest.NewRequest(http.MethodPost, "/api/login", form)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	csrfTok, csrfCk := seedCSRF(t, srv.routes())
	r.Header.Set(csrfHeaderName, csrfTok)
	r.AddCookie(csrfCk)
	srv.routes().ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("POST /api/login: want 303, got %d", w.Code)
	}
	cookie := w.Header().Get("Set-Cookie")
	if !strings.Contains(cookie, auth.CookieName+"=") {
		t.Fatalf("Set-Cookie should include %s, got %q", auth.CookieName, cookie)
	}

	// The cookie value should let us hit /stats now. We can't easily
	// hand-parse the cookie back out of the response header, so just
	// re-fetch by going through the seed path for the second request.
	// Simpler: trust the Set-Cookie parser and reconstruct it from the
	// token we know is in the DB.
	token := ""
	for _, c := range w.Result().Cookies() {
		if c.Name == auth.CookieName {
			token = c.Value
		}
	}
	if token == "" {
		t.Fatalf("login did not set a usable session cookie")
	}

	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/stats", nil)
	r2.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	srv.routes().ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Errorf("GET /stats after login: want 200, got %d", w2.Code)
	}
}

func TestLogoutClearsCookie(t *testing.T) {
	srv, token := newTestServer(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	csrfTok, csrfCk := seedCSRF(t, srv.routes())
	r.Header.Set(csrfHeaderName, csrfTok)
	r.AddCookie(csrfCk)
	srv.routes().ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("POST /api/logout: want 303, got %d", w.Code)
	}

	// After logout the same cookie should be invalid.
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/stats", nil)
	r2.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	srv.routes().ServeHTTP(w2, r2)
	if w2.Code != http.StatusSeeOther {
		t.Errorf("GET /stats after logout: want 303, got %d", w2.Code)
	}
}

func TestRegisterFlowCreatesUserAndLogsIn(t *testing.T) {
	srv, _ := newTestServer(t)
	w := httptest.NewRecorder()
	form := strings.NewReader("name=Bob&email=bob@example.com&password=longenough")
	r := httptest.NewRequest(http.MethodPost, "/api/register", form)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	csrfTok, csrfCk := seedCSRF(t, srv.routes())
	r.Header.Set(csrfHeaderName, csrfTok)
	r.AddCookie(csrfCk)
	srv.routes().ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("POST /api/register: want 303, got %d", w.Code)
	}
	// Find the session cookie.
	var token string
	for _, c := range w.Result().Cookies() {
		if c.Name == auth.CookieName {
			token = c.Value
		}
	}
	if token == "" {
		t.Fatalf("register did not set a session cookie")
	}

	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/stats", nil)
	r2.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	srv.routes().ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Errorf("after register, GET /stats: want 200, got %d", w2.Code)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}