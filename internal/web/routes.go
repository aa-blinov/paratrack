package web

import (
	"io/fs"
	"net/http"
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
	s.registerPublicRecoveryRoutes(mux)
	mux.HandleFunc("GET /login", s.handleLogin)
	mux.HandleFunc("GET /register", s.handleRegister)
	mux.HandleFunc("GET /lang/{code}", s.handleSetLang)
	// Per account from an address, plus a ceiling per address (an office
	// shares one IP).
	mux.Handle("POST /api/login", s.rateLimit("login-ip", 200, time.Minute)(s.rateLimit("login", 10, time.Minute, "email")(http.HandlerFunc(s.handleAPILogin))))
	mux.Handle("POST /api/register", s.rateLimit("register", 30, time.Minute)(http.HandlerFunc(s.handleAPIRegister)))
	mux.HandleFunc("POST /api/logout", s.handleAPILogout)

	staticFS, _ := fs.Sub(assets, "static")
	mux.Handle("GET /static/", cacheStatic(http.StripPrefix("/static/", http.FileServer(http.FS(staticFS)))))
	// Service worker must live at the root path: a script under /static/
	// gets scope /static/ and cannot control the app pages.
	mux.HandleFunc("GET /sw.js", s.handleServiceWorker)
	mux.HandleFunc("GET /static/manifest.webmanifest", s.handleManifest)
	mux.HandleFunc("POST /api/stripe/webhook", s.handleStripeWebhook)
	// Browser error reports are relayed to Sentry only for our configured DSN.
	mux.HandleFunc("POST /sentry-tunnel", s.handleSentryTunnel)

	pages := http.NewServeMux()
	s.registerPageRoutes(pages)
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
