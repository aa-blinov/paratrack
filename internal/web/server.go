// Package web serves paratrack's embedded HTTP UI.
//
// All static assets and templates live under the project-root web/
// directory and are compiled into the binary via go:embed — the result
// is a single executable that opens a complete time-tracker at the
// configured address.
package web

import (
	"net/url"
	"crypto/sha256"
	"encoding/hex"
	"github.com/aa-blinov/paratrack/internal/i18n"
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

// assetVersion is a hash of every embedded static file. It busts the
// browser cache (?v= on asset URLs) and names the service-worker cache,
// so each deploy that changes CSS/JS reaches clients without manual bumps.
var assetVersion = func() string {
	h := sha256.New()
	_ = fs.WalkDir(assets, "static", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := assets.ReadFile(p)
			h.Write([]byte(p))
			h.Write(b)
		}
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:10]
}()

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
	handler := s.routes()
	if initSentry() {
		handler = withSentry(handler)
	}
	s.httpd = &http.Server{
		Addr:              addr,
		Handler:           handler,
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
	"asset":       func(p string) string { return "/static/" + p + "?v=" + assetVersion },
	// tOr translates key, or returns fallback when the key is missing.
	"tOr": func(lang, key, fallback string) string {
		if t := i18n.T(i18n.Lang(lang), key); t != key {
			return t
		}
		return fallback
	},
	"splitComma":  func(s string) []string { return strings.Split(s, ",") },
	// pathesc is for a path segment: urlquery turns spaces into "+", which
	// a path keeps literally ("Только эта" 404'd on names with spaces).
	"pathesc":     url.PathEscape,
	"fmtDurL":     func(lang string, sec int) string { return fmtDurL(i18n.Lang(lang), sec) },
	"colorFor":    colorFor,
	"inkFor":      inkFor,
	"icon":        iconHTML,
	"sentryDSN":   sentryPublicDSN,
	"sentryEnv":   func() string { return sentryEnv },
	"release":     func() string { return "paratrack@" + assetVersion },
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
	// Per account from an address, plus a ceiling per address (an office
	// shares one IP).
	mux.Handle("POST /api/login",    s.rateLimit("login-ip", 200, time.Minute)(s.rateLimit("login", 10, time.Minute, "email")(http.HandlerFunc(s.handleAPILogin))))
	mux.Handle("POST /api/register", s.rateLimit("register", 30, time.Minute)(http.HandlerFunc(s.handleAPIRegister)))
	mux.HandleFunc("POST /api/logout",   s.handleAPILogout)

	staticFS, _ := fs.Sub(assets, "static")
	mux.Handle("GET /static/", cacheStatic(http.StripPrefix("/static/", http.FileServer(http.FS(staticFS)))))
	// Service worker must live at the root path: a script under /static/
	// gets scope /static/ and cannot control the app pages.
	mux.HandleFunc("GET /sw.js", s.handleServiceWorker)
	mux.HandleFunc("GET /static/manifest.webmanifest", s.handleManifest)

	// ----- protected pages -----
	pages := http.NewServeMux()
	pages.HandleFunc("GET /{$}",                    s.mine(s.handleDashboard))
	pages.HandleFunc("GET /stats",                  s.handleStats)
	pages.HandleFunc("GET /export",                 s.handleExport)
	pages.HandleFunc("GET /graph",                  s.module("graph", s.handleGraph))
	pages.HandleFunc("GET /goals",                  s.module("goals", s.handleGoals))
	pages.HandleFunc("GET /tags",                   s.module("tags", s.handleTagsPage))
	pages.HandleFunc("GET /help",                  s.handleHelp)
	pages.HandleFunc("GET /tags-list-fragment",     s.handleTagsFragment)

	// Wave 1: timesheet + saved reports.
	pages.HandleFunc("GET /timesheet",              s.mine(s.handleTimesheet))
	pages.HandleFunc("POST /api/timesheet/cell",    s.mine(s.handleTimesheetCell))
	pages.HandleFunc("POST /api/reports/save",      s.handleSavedReportsCreate)
	pages.HandleFunc("POST /api/reports/{id}/delete", s.handleSavedReportsDelete)

	// Wave 2: API tokens + integrations.
	pages.HandleFunc("GET /settings/tokens",      s.handleSettingsTokens)
	pages.HandleFunc("POST /api/tokens",          s.handleAPITokenCreate)
	pages.HandleFunc("POST /api/tokens/{id}/delete", s.handleAPITokenDelete)
	pages.HandleFunc("GET /integrations",         s.module("integrations", s.handleIntegrations))
	pages.HandleFunc("POST /integrations",        s.module("integrations", s.manage(s.handleIntegrationConnect)))
	pages.HandleFunc("GET /integrations/{id}",    s.module("integrations", s.handleIntegrationDetail))
	pages.HandleFunc("POST /integrations/{id}/sync",   s.module("integrations", s.manage(s.handleIntegrationSync)))
	pages.HandleFunc("POST /integrations/{id}/delete", s.module("integrations", s.manage(s.handleIntegrationDelete)))
	pages.HandleFunc("POST /integrations/start",  s.module("integrations", s.mine(s.handleIntegrationStart)))

	// Wave 3: billable rates + invoices.
	pages.HandleFunc("POST /projects/{slug}/rate", s.manage(s.handleProjectRate))
	pages.HandleFunc("GET /invoices",             s.module("invoices", s.manage(s.handleInvoices)))
	pages.HandleFunc("POST /invoices",            s.module("invoices", s.manage(s.handleInvoiceCreate)))
	pages.HandleFunc("POST /invoices/assign",     s.module("invoices", s.manage(s.handleInvoiceAssignActivity)))
	pages.HandleFunc("GET /invoices/{id}",        s.module("invoices", s.manage(s.handleInvoiceDetail)))
	pages.HandleFunc("POST /invoices/{id}/status", s.module("invoices", s.manage(s.handleInvoiceStatus)))
	pages.HandleFunc("POST /invoices/{id}/delete", s.module("invoices", s.manage(s.handleInvoiceDelete)))
	pages.HandleFunc("GET /invoices/{id}/pdf",     s.module("invoices", s.manage(s.handleInvoicePDF)))
	pages.HandleFunc("GET /invoices/{id}/act",     s.module("invoices", s.manage(s.handleInvoiceAct)))
	pages.HandleFunc("POST /invoices/{id}/edit",    s.module("invoices", s.manage(s.handleInvoiceEdit)))
	pages.HandleFunc("POST /invoices/{id}/rebuild", s.module("invoices", s.manage(s.handleInvoiceRebuild)))
	pages.HandleFunc("POST /invoices/{id}/receipt", s.module("invoices", s.manage(s.handleInvoiceReceipt)))
	pages.HandleFunc("POST /invoices/{id}/send",    s.module("invoices", s.manage(s.handleInvoiceSend)))
	pages.HandleFunc("GET /settings/email-preview", s.manage(s.handleEmailPreview))
	pages.HandleFunc("GET /invoices/{id}/act.pdf", s.module("invoices", s.manage(s.handleInvoiceActPDF)))
	pages.HandleFunc("POST /invoices/{id}/pay",    s.module("invoices", s.manage(s.handleInvoicePayLink)))
	pages.HandleFunc("POST /invoices/{id}/paid",   s.module("invoices", s.manage(s.handleInvoiceMarkPaid)))
	mux.HandleFunc("POST /api/stripe/webhook",    s.handleStripeWebhook)
	// Browser error reports, relayed to Sentry (no CSRF: sent by the SDK
	// from any page, including login; only our own DSN is forwarded).
	mux.HandleFunc("POST /sentry-tunnel",         s.handleSentryTunnel)
	pages.HandleFunc("POST /api/team/stripe",      s.manage(s.handleTeamStripe))

	// Wave 6: payroll + resource scheduling.
	pages.HandleFunc("GET /payroll",              s.module("payroll", s.manage(s.handlePayroll)))
	pages.HandleFunc("POST /payroll",             s.module("payroll", s.manage(s.handlePayrollCreate)))
	pages.HandleFunc("GET /payroll/{id}",         s.module("payroll", s.manage(s.handlePayrollDetail)))
	pages.HandleFunc("POST /payroll/{id}/paid",   s.module("payroll", s.manage(s.handlePayrollPaid)))
	pages.HandleFunc("POST /payroll/{id}/delete", s.module("payroll", s.manage(s.handlePayrollDelete)))
	pages.HandleFunc("POST /api/member/pay",      s.manage(s.handleMemberPay))
	pages.HandleFunc("GET /schedule",             s.module("schedule", s.handleSchedule))
	pages.HandleFunc("POST /api/schedule/cell",   s.module("schedule", s.manage(s.handleScheduleCell)))

	// Wave 7: marketplace + report templates.
	pages.HandleFunc("GET /integrations/marketplace", s.module("integrations", s.handleMarketplace))
	pages.HandleFunc("GET /reports",                  s.module("reports", s.manage(s.handleReports)))
	pages.HandleFunc("GET /reports/run",              s.module("reports", s.manage(s.handleReportRun)))

	// Wave 8: migration from other trackers.
	pages.HandleFunc("GET /import",               s.module("import", s.handleImport))
	pages.HandleFunc("POST /import/preview",      s.module("import", s.handleImportPreview))
	pages.HandleFunc("POST /import/run",          s.module("import", s.handleImportRun))

	// Wave 9: web push.
	pages.HandleFunc("GET /settings/notifications", s.handleNotificationsPage)

	// Projects (Phase 3).
	pages.HandleFunc("GET /projects",                s.handleProjectsList)
	pages.HandleFunc("GET /projects/new",            s.manage(s.handleProjectNew))
	pages.HandleFunc("POST /projects/new",           s.manage(s.handleProjectCreateForm))
	pages.HandleFunc("GET /projects/{slug}",         s.handleProjectDetail)
	pages.HandleFunc("POST /projects/{slug}",        s.manage(s.handleProjectUpdateForm))
	pages.HandleFunc("POST /projects/{slug}/delete", s.manage(s.handleProjectDeleteForm))

	// Settings (Phase 2): team admin, members, invites, profile.
	pages.HandleFunc("GET /settings/team",          s.manage(s.handleTeamSettings))
	pages.HandleFunc("GET /settings/members",       s.manage(s.handleTeamMembers))
	pages.HandleFunc("GET /settings/invites",       s.manage(s.handleTeamInvites))
	pages.HandleFunc("GET /settings/profile",       s.handleSettingsProfile)
	pages.HandleFunc("GET /settings/sections",      s.manage(s.handleSectionsPage))
	pages.HandleFunc("GET /settings/preferences",   s.handlePreferencesPage)
	pages.HandleFunc("GET /welcome",                s.handleWelcome)

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

	api("GET",    "/api/active",                   s.mine(s.handleAPIActive))
	api("GET",    "/api/minibar",                  s.mine(s.handleMiniBar))
	api("GET",    "/api/reports.csv",              s.handleCSV)
	api("POST",   "/api/start",                    s.mine(s.handleStart))
	api("POST",   "/api/sessions/{id}/stop",       s.mine(s.handleStop))
	api("POST",   "/api/sessions/{id}/reopen",     s.mine(s.handleReopen))
	api("POST",   "/api/sessions/{id}/pause",      s.mine(s.handlePause))
	api("POST",   "/api/sessions/{id}/resume",     s.mine(s.handleResume))
	api("POST",   "/api/focus/{name}",             s.mine(s.handleFocus))
	api("POST",   "/api/active/pause-all",         s.mine(s.handlePauseAll))
	api("POST",   "/api/active/stop-all",          s.mine(s.handleStopAll))
	api("PATCH",  "/api/sessions/{id}",            s.handleUpdateSession)
	api("DELETE", "/api/sessions/{id}",            s.handleDeleteSession)
	api("GET",    "/api/goals",                    s.module("goals", s.handleGoalsList))
	api("GET",    "/api/goals/progress",           s.module("goals", s.handleGoalsProgress))
	api("POST",   "/api/goals",                    s.module("goals", s.manage(s.handleGoalsUpsert)))
	api("DELETE", "/api/goals",                    s.module("goals", s.manage(s.handleGoalsDelete)))
	api("GET",    "/api/tags",                     s.handleTagsList)
	api("POST",   "/api/tags",                     s.handleTagsCreate)
	api("DELETE", "/api/tags",                     s.manage(s.handleTagsDelete))
	api("POST",   "/api/sessions/{id}/tags",       s.handleSessionTagAdd)
	api("DELETE", "/api/sessions/{id}/tags",       s.handleSessionTagRemove)

	// Team / profile management (Phase 2).
	api("POST",   "/api/team/rename",              s.manage(s.handleAPITeamRename))
	api("POST",   "/api/team/currency",            s.manage(s.handleAPITeamCurrency))
	api("POST",   "/api/team/requisites",          s.manage(s.handleAPITeamRequisites))
	api("POST",   "/api/team/modules",             s.manage(s.handleAPITeamModules))
	api("POST",   "/api/team/billing",             s.manage(s.handleAPITeamBilling))
	api("POST",   "/api/team/logo",                s.manage(s.handleAPITeamLogo))
	api("POST",   "/api/me/preferences",           s.handleAPIPreferences)
	api("POST",   "/api/team/create",              s.handleAPITeamCreate)
	api("POST",   "/api/team/switch",              s.handleAPITeamSwitch)
	api("POST",   "/api/team/delete",              s.handleAPITeamDelete)
	api("DELETE", "/api/team",                     s.handleAPITeamDelete)
	api("POST",   "/api/team/invites",             s.manage(s.handleAPIInviteCreate))
	api("POST",   "/api/team/invites/{token}/revoke", s.manage(s.handleAPIInviteRevoke))
	api("DELETE", "/api/team/invites/{token}",     s.manage(s.handleAPIInviteRevoke))
	api("POST",   "/api/team/members/{id}/remove", s.handleAPIMemberRemove)
	api("POST",   "/api/team/members/{id}/role",   s.manage(s.handleAPIMemberRole))
	api("POST",   "/api/team/transfer",            s.manage(s.handleAPITeamTransfer))
	api("POST",   "/api/invites/{token}/accept",   s.handleAPIInviteAccept)
	api("POST",   "/api/profile",                  s.handleAPIProfileUpdate)
	api("POST",   "/api/profile/password",         s.handleAPIProfilePassword)

	// Projects (Phase 2 of the projects rollout).
	api("GET",    "/api/projects",                 s.handleAPIProjectsList)
	api("POST",   "/api/projects",                 s.manage(s.handleAPIProjectCreate))
	api("PATCH",  "/api/projects/{id}",            s.manage(s.handleAPIProjectUpdate))
	api("DELETE", "/api/projects/{id}",            s.manage(s.handleAPIProjectDelete))
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
	pages.HandleFunc("GET /settings/webhooks",    s.manage(s.handleWebhooksPage))
	pages.HandleFunc("POST /api/webhooks",        s.manage(s.handleWebhookCreate))
	pages.HandleFunc("POST /api/webhooks/{id}/delete", s.manage(s.handleWebhookDelete))
	pages.HandleFunc("GET /settings/audit",       s.manage(s.handleAuditPage))

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
		start := userNow(r)
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