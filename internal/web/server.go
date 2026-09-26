// Package web serves paratrack's embedded HTTP UI.
//
// All static assets and templates live under the project-root web/
// directory and are compiled into the binary via go:embed — the result
// is a single executable that opens a complete time-tracker at the
// configured address.
package web

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/auth"
	"github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/mail"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/teams"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

//go:embed templates/*.html static/*
var assets embed.FS

// Server is the HTTP front-end for paratrack.
type Server struct {
	db    *db.DB
	auth  *auth.Service
	teams *teams.Service
	mailer mail.Sender
	addr  string
	tmpl  *template.Template
	httpd *http.Server
}

// New constructs a Server but does not yet bind the socket.
func New(database *db.DB, addr string) (*Server, error) {
	tmpl, err := parseTemplates()
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	s := &Server{
		db:    database,
		auth:  auth.NewService(database),
		teams: teams.NewService(database),
		mailer: mail.FromEnv(),
		addr:  addr,
		tmpl:  tmpl,
	}
	s.httpd = &http.Server{
		Addr:              addr,
		Handler:           s.routes(),
		ReadHeaderTimeout:  5 * time.Second,
	}
	return s, nil
}

// ListenAndServe blocks until the server stops.
func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	log.Printf("paratrack web: listening on http://%s", ln.Addr())
	if err := s.httpd.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error { return s.httpd.Shutdown(ctx) }

// parseTemplates parses every *.html under templates/. Files define
// blocks ("base", "content") so child pages compose into the shared
// shell automatically.
func parseTemplates() (*template.Template, error) {
	root := template.New("").Funcs(funcMap)
	matches, err := fs.Glob(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no templates found")
	}
	for _, name := range matches {
		b, err := assets.ReadFile(name)
		if err != nil {
			return nil, err
		}
		if _, err := root.New(strings.TrimPrefix(name, "templates/")).Parse(string(b)); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	return root, nil
}

var funcMap = template.FuncMap{
	// fmtDuration is the shared smart formatter from dto.go — keep the
	// template name in lock-step so cards and tables always agree.
	"fmtDuration": fmtDuration,
	"colorFor":    colorFor,
	"inkFor":      inkFor,
	"icon":        iconHTML,
}

// iconHTML renders a Lucide glyph from the vendored sprite:
//
//	{{icon "play"}}              → <svg class="icon"><use href="…#i-play"/></svg>
//	{{icon "play" "icon-lg"}}
//
// Names are Lucide kebab-case (play, pause, square, target, tag, …).
// The sprite lives at /static/icons.svg; each <symbol> is prefixed i-.
func iconHTML(name string, classes ...string) template.HTML {
	cls := "icon"
	if len(classes) > 0 && classes[0] != "" {
		cls = classes[0]
	}
	// Name is constrained to the Lucide kebab set so it can't inject.
	for _, r := range name {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' {
			return ""
		}
	}
	return template.HTML(`<svg class="` + cls + `" aria-hidden="true"><use href="/static/icons.svg#i-` + name + `"></use></svg>`)
}

// routes wires every HTTP route the server exposes. Go 1.22+ pattern
// routing means we don't pull in a third-party router.
//
// Phase 1 wire-up splits the routes into three buckets:
//
//   - Public — anyone can hit (login form, registration form, logout,
//     static files).
//   - Auth page — page routes redirect unauthenticated visitors to
//     /login so the browser can flow through the form normally.
//   - Auth API — every /api/* call (other than the auth flow itself)
//     returns a JSON body on auth failure, so HTMX / curl / Playwright
//     all behave sensibly.
//
// The middleware sets User and Team on r.Context() before dispatching
// to the actual handler.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	pageAuth := s.requireAuth(pageRedirect)
	apiAuth := s.requireAuth(apiUnauthorized)

	// ----- public -----
	mux.HandleFunc("GET /login",         s.handleLogin)
	mux.HandleFunc("GET /register",      s.handleRegister)
	mux.HandleFunc("GET /lang/{code}",     s.handleSetLang)
	mux.Handle("POST /api/login",    s.rateLimit("login", 10, time.Minute)(http.HandlerFunc(s.handleAPILogin)))
	mux.Handle("POST /api/register", s.rateLimit("register", 5, time.Minute)(http.HandlerFunc(s.handleAPIRegister)))
	mux.HandleFunc("POST /api/logout",   s.handleAPILogout)

	staticFS, _ := fs.Sub(assets, "static")
	mux.Handle("GET /static/", cacheStatic(http.StripPrefix("/static/", http.FileServer(http.FS(staticFS)))))
	// Service worker must live at the root path: a script under /static/
	// gets scope /static/ and cannot control the app pages.
	mux.HandleFunc("GET /sw.js", s.handleServiceWorker)

	// ----- protected pages -----
	pages := http.NewServeMux()
	pages.HandleFunc("GET /{$}",                    s.handleDashboard)
	pages.HandleFunc("GET /stats",                  s.handleStats)
	pages.HandleFunc("GET /graph",                  s.handleGraph)
	pages.HandleFunc("GET /goals",                  s.handleGoals)
	pages.HandleFunc("GET /tags",                   s.handleTagsPage)
	pages.HandleFunc("GET /help",                  s.handleHelp)
	pages.HandleFunc("GET /tags-list-fragment",     s.handleTagsFragment)

	// Wave 1: timesheet + saved reports.
	pages.HandleFunc("GET /timesheet",              s.handleTimesheet)
	pages.HandleFunc("POST /api/timesheet/cell",    s.handleTimesheetCell)
	pages.HandleFunc("POST /api/reports/save",      s.handleSavedReportsCreate)
	pages.HandleFunc("POST /api/reports/{id}/delete", s.handleSavedReportsDelete)

	// Wave 2: API tokens + integrations.
	pages.HandleFunc("GET /settings/tokens",      s.handleSettingsTokens)
	pages.HandleFunc("POST /api/tokens",          s.handleAPITokenCreate)
	pages.HandleFunc("POST /api/tokens/{id}/delete", s.handleAPITokenDelete)
	pages.HandleFunc("GET /integrations",         s.handleIntegrations)
	pages.HandleFunc("POST /integrations",        s.handleIntegrationConnect)
	pages.HandleFunc("GET /integrations/{id}",    s.handleIntegrationDetail)
	pages.HandleFunc("POST /integrations/{id}/sync",   s.handleIntegrationSync)
	pages.HandleFunc("POST /integrations/{id}/delete", s.handleIntegrationDelete)
	pages.HandleFunc("POST /integrations/start",  s.handleIntegrationStart)

	// Wave 3: billable rates + invoices.
	pages.HandleFunc("POST /projects/{slug}/rate", s.handleProjectRate)
	pages.HandleFunc("GET /invoices",             s.handleInvoices)
	pages.HandleFunc("POST /invoices",            s.handleInvoiceCreate)
	pages.HandleFunc("GET /invoices/{id}",        s.handleInvoiceDetail)
	pages.HandleFunc("POST /invoices/{id}/status", s.handleInvoiceStatus)
	pages.HandleFunc("POST /invoices/{id}/delete", s.handleInvoiceDelete)
	pages.HandleFunc("GET /invoices/{id}/pdf",     s.handleInvoicePDF)
	pages.HandleFunc("POST /invoices/{id}/pay",    s.handleInvoicePayLink)
	pages.HandleFunc("POST /invoices/{id}/paid",   s.handleInvoiceMarkPaid)
	mux.HandleFunc("POST /api/stripe/webhook",    s.handleStripeWebhook)
	pages.HandleFunc("POST /api/team/stripe",      s.handleTeamStripe)

	// Wave 6: payroll + resource scheduling.
	pages.HandleFunc("GET /payroll",              s.handlePayroll)
	pages.HandleFunc("POST /payroll",             s.handlePayrollCreate)
	pages.HandleFunc("GET /payroll/{id}",         s.handlePayrollDetail)
	pages.HandleFunc("POST /payroll/{id}/paid",   s.handlePayrollPaid)
	pages.HandleFunc("POST /payroll/{id}/delete", s.handlePayrollDelete)
	pages.HandleFunc("POST /api/member/pay",      s.handleMemberPay)
	pages.HandleFunc("GET /schedule",             s.handleSchedule)
	pages.HandleFunc("POST /api/schedule/cell",   s.handleScheduleCell)

	// Wave 7: marketplace + report templates.
	pages.HandleFunc("GET /integrations/marketplace", s.handleMarketplace)
	pages.HandleFunc("GET /reports",                  s.handleReports)
	pages.HandleFunc("GET /reports/run",              s.handleReportRun)

	// Wave 8: migration from other trackers.
	pages.HandleFunc("GET /import",               s.handleImport)
	pages.HandleFunc("POST /import/preview",      s.handleImportPreview)
	pages.HandleFunc("POST /import/run",          s.handleImportRun)

	// Wave 9: web push.
	pages.HandleFunc("GET /settings/notifications", s.handleNotificationsPage)

	// Projects (Phase 3).
	pages.HandleFunc("GET /projects",                s.handleProjectsList)
	pages.HandleFunc("GET /projects/new",            s.handleProjectNew)
	pages.HandleFunc("POST /projects/new",           s.handleProjectCreateForm)
	pages.HandleFunc("GET /projects/{slug}",         s.handleProjectDetail)
	pages.HandleFunc("POST /projects/{slug}",        s.handleProjectUpdateForm)
	pages.HandleFunc("POST /projects/{slug}/delete", s.handleProjectDeleteForm)

	// Settings (Phase 2): team admin, members, invites, profile.
	pages.HandleFunc("GET /settings/team",          s.handleTeamSettings)
	pages.HandleFunc("GET /settings/members",       s.handleTeamMembers)
	pages.HandleFunc("GET /settings/invites",       s.handleTeamInvites)
	pages.HandleFunc("GET /settings/profile",       s.handleSettingsProfile)

	// Public invite-accept page (auth required to actually click Join).
	pages.HandleFunc("GET /invites/{token}",        s.handleInviteAcceptPage)
	mux.Handle("/", pageAuth(pages))

	// ----- protected /api/* -----
	// All /api/* routes are registered directly on the top-level mux
	// with their full path. The apiAuth middleware wraps each one so
	// auth failures return JSON 401 (not a 303 to /login). This avoids
	// the StripPrefix dance that sub-muxing would require under Go 1.22
	// pattern matching.
	api := func(method, path string, h http.HandlerFunc) {
		mux.Handle(method+" "+path, apiAuth(h))
	}

	api("GET",    "/api/active",                   s.handleAPIActive)
	api("GET",    "/api/reports.csv",              s.handleCSV)
	api("POST",   "/api/start",                    s.handleStart)
	api("POST",   "/api/sessions/{id}/stop",       s.handleStop)
	api("POST",   "/api/sessions/{id}/pause",      s.handlePause)
	api("POST",   "/api/sessions/{id}/resume",     s.handleResume)
	api("POST",   "/api/focus/{name}",             s.handleFocus)
	api("PATCH",  "/api/sessions/{id}",            s.handleUpdateSession)
	api("DELETE", "/api/sessions/{id}",            s.handleDeleteSession)
	api("GET",    "/api/goals",                    s.handleGoalsList)
	api("GET",    "/api/goals/progress",           s.handleGoalsProgress)
	api("POST",   "/api/goals",                    s.handleGoalsUpsert)
	api("DELETE", "/api/goals",                    s.handleGoalsDelete)
	api("GET",    "/api/tags",                     s.handleTagsList)
	api("POST",   "/api/tags",                     s.handleTagsCreate)
	api("DELETE", "/api/tags",                     s.handleTagsDelete)
	api("POST",   "/api/sessions/{id}/tags",       s.handleSessionTagAdd)
	api("DELETE", "/api/sessions/{id}/tags",       s.handleSessionTagRemove)

	// Team / profile management (Phase 2).
	api("POST",   "/api/team/rename",              s.handleAPITeamRename)
	api("POST",   "/api/team/create",              s.handleAPITeamCreate)
	api("POST",   "/api/team/switch",              s.handleAPITeamSwitch)
	api("POST",   "/api/team/delete",              s.handleAPITeamDelete)
	api("DELETE", "/api/team",                     s.handleAPITeamDelete)
	api("POST",   "/api/team/invites",             s.handleAPIInviteCreate)
	api("POST",   "/api/team/invites/{token}/revoke", s.handleAPIInviteRevoke)
	api("DELETE", "/api/team/invites/{token}",     s.handleAPIInviteRevoke)
	api("POST",   "/api/team/members/{id}/remove", s.handleAPIMemberRemove)
	api("POST",   "/api/invites/{token}/accept",   s.handleAPIInviteAccept)
	api("POST",   "/api/profile",                  s.handleAPIProfileUpdate)
	api("POST",   "/api/profile/password",         s.handleAPIProfilePassword)

	// Projects (Phase 2 of the projects rollout).
	api("GET",    "/api/projects",                 s.handleAPIProjectsList)
	api("POST",   "/api/projects",                 s.handleAPIProjectCreate)
	api("PATCH",  "/api/projects/{id}",            s.handleAPIProjectUpdate)
	api("DELETE", "/api/projects/{id}",            s.handleAPIProjectDelete)
	api("POST",   "/api/activities/{id}/project",  s.handleAPIAssignActivityProject)

	// Account recovery + backfill (SaaS).
	// forgot/reset are PUBLIC (like /api/login) — the user is locked out.
	// Rate-limited; CSRF still applies via the global wrapper.
	mux.HandleFunc("GET /forgot-password",         s.handleForgotPassword)
	mux.HandleFunc("GET /reset-password",          s.handleResetPassword)
	mux.Handle("POST /api/password/forgot", s.rateLimit("pwforgot", 5, time.Minute)(http.HandlerFunc(s.handleAPIPasswordForgot)))
	mux.Handle("POST /api/password/reset",  s.rateLimit("pwreset", 10, time.Minute)(http.HandlerFunc(s.handleAPIPasswordReset)))
	api("POST",   "/api/sessions/backfill",        s.handleBackfill)

	// Wave 2 JSON used by the browser extension (Bearer API token).
	api("GET",    "/api/me",                       s.handleAPIMe)
	api("GET",    "/api/external-tasks",           s.handleAPIExternalTasks)

	// Wave 9: web push (JSON + form).
	api("GET",    "/api/push/key",                s.handlePushKey)
	api("POST",   "/api/push/subscribe",          s.handlePushSubscribe)
	api("POST",   "/api/push/unsubscribe",        s.handlePushUnsubscribe)

	// Wave 4: API v1 (Bearer or cookie).
	api("GET",    "/api/v1/sessions",              s.handleAPIv1Sessions)
	api("POST",   "/api/v1/sessions",              s.handleAPIv1Sessions)
	api("PATCH",  "/api/v1/sessions/{id}",         s.handleAPIv1Session)
	api("DELETE", "/api/v1/sessions/{id}",         s.handleAPIv1Session)
	api("GET",    "/api/v1/projects",              s.handleAPIv1Projects)
	api("GET",    "/api/v1/reports/summary",       s.handleAPIv1Report)

	// Wave 4: webhooks + audit pages.
	pages.HandleFunc("GET /settings/webhooks",    s.handleWebhooksPage)
	pages.HandleFunc("POST /api/webhooks",        s.handleWebhookCreate)
	pages.HandleFunc("POST /api/webhooks/{id}/delete", s.handleWebhookDelete)
	pages.HandleFunc("GET /settings/audit",       s.handleAuditPage)

	// Wave 4: OIDC SSO (public).
	mux.HandleFunc("GET /sso/login",     s.handleSSOLogin)
	mux.HandleFunc("GET /sso/callback",  s.handleSSOCallback)

	// Auth endpoints get a tighter rate limit on top of CSRF. The login
	// form itself is a page (GET) so it stays unlimited; the POST is
	// what we throttle.
	// NOTE: rate-limited wrappers are applied inside routes via the
	// helper below — see wrapAuthRateLimit.
	return securityHeaders(csrfProtect(logRequests(mux)))
}

// logRequests is a tiny middleware: prints method, path, status, latency.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(ww, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, ww.status, time.Since(start))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// DB access helpers (re-export so handlers don't repeat the import).
type sqlQuerier = sql.DB // alias for documentation only
var _ = model.Session{}
var _ = timeparse.Period{}