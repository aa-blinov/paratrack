package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// ---------------------------------------------------------------------------
// CSRF — double-submit cookie.
//
// A non-HttpOnly `paratrack_csrf` cookie holds a random token. Every
// state-changing request (POST/PATCH/DELETE) must echo it either as the
// `csrf_token` form field or the `X-CSRF-Token` header. HTMX sends the
// header via hx-headers on <body>; plain forms carry a hidden input.
//
// The cookie is deliberately readable by JS so app.js can populate
// hx-headers without a server round-trip. An attacker on another origin
// cannot read it (SOP) and cannot make the browser send it on a
// cross-site form POST that we then accept — the header/field must
// match, and cross-site pages cannot read the cookie value.
// ---------------------------------------------------------------------------

const (
	csrfCookieName = "paratrack_csrf"
	csrfFieldName  = "csrf_token"
	csrfHeaderName = "X-CSRF-Token"
	csrfCtxKey     = ctxKey(100)
	maxRequestBody = 10 << 20
)

// ensureCSRF returns the request's CSRF token, issuing one if needed.
// csrfProtect stashes the token on the request context so the page
// renderer and the validator agree on a single value (calling this
// twice without that would mint two cookies and the form token would
// not match the one the browser kept).
func ensureCSRF(w http.ResponseWriter, r *http.Request) string {
	if r != nil {
		if t, ok := r.Context().Value(csrfCtxKey).(string); ok && t != "" {
			return t
		}
	}
	return issueCSRF(w, r)
}

// issueCSRF reads the cookie or mints a fresh token and sets the cookie.
func issueCSRF(w http.ResponseWriter, r *http.Request) string {
	if r != nil {
		if c, err := r.Cookie(csrfCookieName); err == nil && c.Value != "" {
			return c.Value
		}
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	token := base64.RawURLEncoding.EncodeToString(b[:])
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false, // JS must read it for hx-headers
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(appmodel.SessionTTL.Seconds()),
		Secure:   r != nil && isSecureRequest(r),
	})
	return token
}

// csrfProtect wraps a handler and rejects state-changing requests whose
// CSRF token is missing or mismatched.
func csrfProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := issueCSRF(w, r)
		r = r.WithContext(context.WithValue(r.Context(), csrfCtxKey, token))
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
			next.ServeHTTP(w, r)
			return
		}
		if csrfExempt(r) {
			next.ServeHTTP(w, r)
			return
		}
		sent := r.Header.Get(csrfHeaderName)
		if sent == "" {
			// Form-encoded bodies carry it as a field; ParseForm is safe
			// here — body size is bounded by net/http defaults.
			if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
				// File uploads (the invoice logo): bounded so a big body
				// can't sit in memory or temp files.
				r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
				_ = r.ParseMultipartForm(2 << 20)
			} else {
				_ = r.ParseForm()
			}
			sent = r.PostForm.Get(csrfFieldName)
		}
		if !csrfTokensEqual(token, sent) {
			writeCSRFError(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// limitRequestBody bounds every request body before CSRF or form parsing.
// Per-endpoint decoders may apply smaller limits for structured payloads.
func limitRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.Body != http.NoBody {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		}
		next.ServeHTTP(w, r)
	})
}

// csrfExempt: requests a browser can't forge from another site, so the
// cookie token has nothing to protect. A Bearer header can't be set
// cross-origin without a CORS preflight we never grant (API tokens: the
// extension, the CLI, scripts). Stripe signs its webhook itself; the
// Sentry tunnel only relays envelopes for our own DSN.
func csrfExempt(r *http.Request) bool {
	if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		return true
	}
	switch r.URL.Path {
	case "/api/stripe/webhook", "/sentry-tunnel":
		return true
	}
	return false
}

func csrfTokensEqual(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func writeCSRFError(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"csrf token missing or invalid"}`))
		return
	}
	http.Error(w, "invalid or missing CSRF token — reload the page and try again", http.StatusForbidden)
}

// ---------------------------------------------------------------------------
// Security headers
// ---------------------------------------------------------------------------

// securityHeaders sets conservative defaults for a same-origin SPA-less
// app. The CSP allows our vendored scripts, inline handlers used by
// HTMX/Alpine wiring in base.html, and the ECharts canvas (data:).
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		// Avoid forwarding query credentials such as password-reset tokens.
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		// 'unsafe-inline' for script is required by the inline HTMX glue in
		// base.html and Alpine's x-on attributes; 'unsafe-eval' is required
		// by Alpine 3's expression compiler (new Function). Tighten once
		// those move into app.js / a CSP build of Alpine.
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline' 'unsafe-eval'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; "+
				"font-src 'self'; "+
				"connect-src 'self'; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self'")
		if isSecureRequest(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// isSecureRequest reports whether the client spoke TLS directly or via
// a configured, trusted TLS-terminating proxy that sets X-Forwarded-Proto.
func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if secure, _ := r.Context().Value(securePublicOriginKey{}).(bool); secure {
		return true
	}
	if len(trustedProxyPrefixes(r)) == 0 {
		return false
	}
	proto := strings.TrimSpace(strings.SplitN(r.Header.Get("X-Forwarded-Proto"), ",", 2)[0])
	return strings.EqualFold(proto, "https")
}
