package web

import (
	"io/fs"
	"net/http"
	"strings"
	"time"
)

// routes wires every HTTP route the server exposes. Go 1.22+ pattern
// routing means we don't pull in a third-party router.
//
// Routes are registered in three groups:
//
//   - Public — anyone can hit (login form, registration form, logout,
//     static files).
//   - Auth page — page routes redirect unauthenticated visitors to
//     /login so the browser can flow through the form normally.
//   - Auth API — every /api/* call (other than the auth flow itself)
//     returns a JSON body on auth failure, so fetch / curl / Playwright
//     all behave sensibly.
//
// The middleware sets User and Team on r.Context() before dispatching
// to the actual handler.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	apiAuth := s.requireAuth(apiUnauthorized)

	// ----- public -----
	s.registerPublicRecoveryRoutes(mux)
	mux.HandleFunc("GET /login", s.handleLogin)
	mux.HandleFunc("GET /login/2fa", s.handleLogin2FA)
	mux.HandleFunc("GET /verify-email", s.handleVerifyEmail)
	mux.Handle("POST /api/login/2fa", s.rateLimit("login-2fa-ip", 60, time.Minute)(http.HandlerFunc(s.handleAPILogin2FA)))
	mux.HandleFunc("GET /register", s.handleRegister)
	mux.HandleFunc("GET /lang/{code}", s.handleSetLang)
	// Per account from an address, plus a ceiling per address (an office
	// shares one IP), plus a per-account ceiling across all addresses: a
	// botnet spreading over IPs must not outvote the per-address budget.
	mux.Handle("POST /api/login",
		s.rateLimitKeyed("login-account", 20, time.Minute, func(r *http.Request) string {
			return strings.ToLower(strings.TrimSpace(r.FormValue("email")))
		})(
			s.rateLimit("login-ip", 200, time.Minute)(
				s.rateLimit("login", 10, time.Minute, "email")(http.HandlerFunc(s.handleAPILogin)))))
	// Registration is throttled per address and per mailbox: a botnet can
	// rotate addresses, but mass-created accounts are noise in every
	// workspace list from the first minute.
	mux.Handle("POST /api/register",
		s.rateLimit("register", 30, time.Minute)(
			s.rateLimit("register-email", 10, time.Minute, "email")(http.HandlerFunc(s.handleAPIRegister))))
	mux.HandleFunc("POST /api/logout", s.handleAPILogout)

	staticFS, _ := fs.Sub(assets, "static")
	mux.Handle("GET /static/", cacheStatic(http.StripPrefix("/static/", http.FileServer(http.FS(staticFS)))))
	// Service worker must live at the root path: a script under /static/
	// gets scope /static/ and cannot control the app pages.
	mux.HandleFunc("GET /sw.js", s.handleServiceWorker)
	mux.HandleFunc("GET /static/manifest.webmanifest", s.handleManifest)
	mux.HandleFunc("POST /api/stripe/webhook", s.handleStripeWebhook)
	// Browser error reports are relayed to Sentry only for our configured DSN.
	// The relay is rate-limited like any other public POST: an open tunnel is
	// a way to drain the Sentry quota with garbage envelopes.
	mux.Handle("POST /sentry-tunnel", s.rateLimit("sentry-tunnel", 60, time.Minute)(http.HandlerFunc(s.handleSentryTunnel)))
	// A client's link to an invoice's PDF: the token is the key, and the rate limit keeps
	// guessing slow (FIN-13).
	mux.Handle("GET /share/invoices/{token}", s.rateLimit("share-invoice", 120, time.Minute)(http.HandlerFunc(s.handleSharedInvoicePDF)))
	// The company website's enquiry route. Its only credential is the intake key header,
	// so it is outside the session and API token middleware, and it answers the browser's
	// cross-origin preflight itself.
	mux.Handle("POST /api/leads/intake", s.leadIntakeHandler())
	mux.Handle("OPTIONS /api/leads/intake", leadIntakeCORS(http.HandlerFunc(leadIntakePreflight)))

	pageRoutes := http.NewServeMux()
	s.registerPageRoutes(pageRoutes)
	pages := s.shellNotFound(s.requireSectionsChosen(pageRoutes))
	pageAuth := s.requireAuth(s.pageAuthFailure(pageRoutes))
	mux.Handle("/", pageAuth(pages))
	s.registerProtectedAPIRoutes(mux, apiAuth)

	// Public OIDC sign-in callbacks.
	mux.HandleFunc("GET /sso/login", s.handleSSOLogin)
	mux.HandleFunc("GET /sso/callback", s.handleSSOCallback)

	// Auth endpoints get a tighter rate limit on top of CSRF. The login
	// form itself is a page (GET) so it stays unlimited; the POST is
	// what we throttle.
	handler := withDefaultTimezone(securityHeaders(limitRequestBody(csrfProtect(logRequests(mux, s.logger, mux)))), s.config.DefaultTimezone)
	handler = withSecurePublicOrigin(handler, s.config.PublicURL)
	return withRequestClock(withTrustedProxies(handler, s.config.TrustedProxies), s.config.Now)
}

func (s *Server) registerPublicRecoveryRoutes(mux *http.ServeMux) {
	// Recovery must remain available to users who cannot authenticate. CSRF
	// protection still applies globally, and the POSTs have dedicated limits.
	mux.HandleFunc("GET /forgot-password", s.handleForgotPassword)
	mux.HandleFunc("GET /reset-password", s.handleResetPassword)
	mux.Handle("POST /api/password/forgot", s.rateLimit("pwforgot", 5, time.Minute)(http.HandlerFunc(s.handleAPIPasswordForgot)))
	mux.Handle("POST /api/password/reset", s.rateLimit("pwreset", 10, time.Minute)(http.HandlerFunc(s.handleAPIPasswordReset)))
}
