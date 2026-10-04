package web

import (
	"context"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/app"
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/audit"
	"github.com/aa-blinov/paratrack/internal/auth"
	dbpkg "github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/mail"
	"github.com/aa-blinov/paratrack/internal/testutil"
)

// newTestServer builds a Server backed by a temp DB and seeds one
// user + personal team, returning the server, the seeded user, and a
// freshly minted session token so tests can hit authed paths.
func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	d, err := testutil.OpenTest(t)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	srv, err := newServerForTest(d, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("server: %v", err)
	}

	auditService, err := audit.New(d)
	if err != nil {
		t.Fatalf("audit service: %v", err)
	}
	authSvc, err := auth.NewService(auth.Dependencies{
		Users: d, Sessions: d, Resets: d, Tokens: d, Memberships: d,
		Now: time.Now, Audit: auditService, Logger: log.Default(),
	})
	if err != nil {
		t.Fatalf("auth service: %v", err)
	}
	sess, _, err := authSvc.RegisterAndStartSession(context.Background(), appmodel.RegistrationRequest{
		Email: "alice@example.com", Password: "longenough", Name: "Alice", TeamName: "Alice workspace",
	})
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return srv, sess.Token
}

func newServerForTest(database *dbpkg.DB, addr string) (*Server, error) {
	services, err := app.NewServices(database, app.Config{Logger: log.Default(), Now: time.Now})
	if err != nil {
		return nil, err
	}
	return New(Dependencies{
		Billing: services.Billing,
		Auth: AuthenticationDependencies{
			Identity: services.Auth, SignIn: services.Auth, Recovery: services.Auth,
			Profile: services.Auth, APITokens: services.Auth,
		},
		AuditLog: services.AuditLog,
		Teams: TeamDependencies{
			Directory: services.Teams, Invitations: services.Teams,
			Settings: services.Teams, Administration: services.Teams,
		},
		TeamOps: services.TeamOps,
		Tracking: TrackingDependencies{
			Queries: services.Tracking, Commands: services.Tracking,
		},
		TrackingOps: services.TrackingOps,
		Imports:     services.Imports,
		Integrations: IntegrationDependencies{
			Queries: services.Integrations, Commands: services.Integrations,
		},
		Invoicing: InvoiceDependencies{
			Queries: services.Invoicing, Drafts: services.Invoicing,
			PaymentLinks: services.Invoicing,
		},
		InvoiceDocuments: services.InvoiceDocuments,
		Payroll:          services.Payroll,
		PayrollPaid:      services.PayrollPaid,
		Preferences:      services.Preferences,
		Scheduling:       services.Scheduling,
		Projects:         ProjectDependencies{Queries: services.Projects, Commands: services.Projects},
		ProjectPages:     services.ProjectPages,
		Reports:          services.Reports,
		ReportBuilder:    services.ReportBuilder,
		Dashboard:        services.Dashboard,
		MemberAdmin:      services.MemberAdmin,
		Push:             services.Push,
		Tagging: TagDependencies{
			Queries: services.Tagging, Commands: services.Tagging,
		},
		SessionTags: services.Tagging,
		Goals:       services.Goals,
		Webhooks:    services.Webhooks,
		MailQueue:   services.MailQueue,
	}, addr, Config{Logger: log.Default(), Now: time.Now}, RuntimeDependencies{
		OIDC:                  nil,
		Mailer:                mail.LogSender{},
		MailQueueWorker:       services.MailQueue,
		WebhookDeliveryWorker: services.Webhooks,
	})
}

func TestPublicLoginPageAccessibleWithoutAuth(t *testing.T) {
	srv, _ := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/login", nil)
	srv.routes().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("GET /login: want 200, got %d", w.Code)
	}
	if want := i18n.T(i18n.Default, "title.Log in"); !strings.Contains(w.Body.String(), want) {
		t.Errorf("body should contain %q, got first 200 chars: %q", want,
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
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	srv.routes().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("GET /stats with cookie: want 200, got %d body=%q",
			w.Code, w.Body.String())
	}
}

func TestAuthenticatedWorkflowAuditReceivesCurrentTeamScope(t *testing.T) {
	srv, token := newTestServer(t)
	user, err := srv.services.Auth.Identity.AuthenticateSessionToken(context.Background(), token)
	if err != nil {
		t.Fatalf("authenticate test session: %v", err)
	}
	memberships, err := srv.services.Teams.Directory.MembershipsForUser(context.Background(), user.ID)
	if err != nil || len(memberships) == 0 {
		t.Fatalf("load test user's workspace: memberships=%d err=%v", len(memberships), err)
	}

	csrfToken, csrfCookie := seedCSRF(t, srv.routes())
	req := csrfRequest(http.MethodPost, "/api/profile/password",
		"current_password=longenough&new_password=longerpassword", csrfToken,
		csrfCookie, &http.Cookie{Name: sessionCookieName, Value: token})
	response := httptest.NewRecorder()
	srv.routes().ServeHTTP(response, req)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("change password status = %d, want %d; body=%q", response.Code, http.StatusSeeOther, response.Body.String())
	}

	entries, err := srv.services.AuditLog.List(context.Background(), memberships[0].Team.ID, 20)
	if err != nil {
		t.Fatalf("list workspace audit: %v", err)
	}
	for _, entry := range entries {
		if entry.Action == "auth.password_change" && entry.TeamID == memberships[0].Team.ID {
			return
		}
	}
	t.Fatalf("password change audit entry missing for team %d", memberships[0].Team.ID)
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
	if !strings.Contains(cookie, sessionCookieName+"=") {
		t.Fatalf("Set-Cookie should include %s, got %q", sessionCookieName, cookie)
	}

	// The cookie value should let us hit /stats now. We can't easily
	// hand-parse the cookie back out of the response header, so just
	// re-fetch by going through the seed path for the second request.
	// Simpler: trust the Set-Cookie parser and reconstruct it from the
	// token we know is in the DB.
	token := ""
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookieName {
			token = c.Value
		}
	}
	if token == "" {
		t.Fatalf("login did not set a usable session cookie")
	}

	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/stats", nil)
	r2.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	srv.routes().ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Errorf("GET /stats after login: want 200, got %d", w2.Code)
	}
}

func TestLogoutClearsCookie(t *testing.T) {
	srv, token := newTestServer(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
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
	r2.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
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
		if c.Name == sessionCookieName {
			token = c.Value
		}
	}
	if token == "" {
		t.Fatalf("register did not set a session cookie")
	}

	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/stats", nil)
	r2.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
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

// Server-to-server and token callers pass without a CSRF token; a plain
// cookie POST still needs one.
func TestCSRFExemptions(t *testing.T) {
	h := csrfProtect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, c := range []struct {
		path, auth string
		want       int
	}{
		{"/api/stripe/webhook", "", 204},
		{"/sentry-tunnel", "", 204},
		{"/api/start", "Bearer pt_x", 204},
		{"/api/start", "", 403},
	} {
		r := httptest.NewRequest("POST", c.path, nil)
		if c.auth != "" {
			r.Header.Set("Authorization", c.auth)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != c.want {
			t.Errorf("POST %s auth=%q: %d, want %d", c.path, c.auth, rec.Code, c.want)
		}
	}
}
