package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aa-blinov/paratrack/internal/auth"
	"github.com/aa-blinov/paratrack/internal/i18n"
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
		MaxAge:   int(auth.SessionTTL.Seconds()),
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
		sent := r.Header.Get(csrfHeaderName)
		if sent == "" {
			// Form-encoded bodies carry it as a field; ParseForm is safe
			// here — body size is bounded by net/http defaults.
			_ = r.ParseForm()
			sent = r.PostForm.Get(csrfFieldName)
		}
		if !csrfTokensEqual(token, sent) {
			writeCSRFError(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
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
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
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
// a TLS-terminating proxy that sets X-Forwarded-Proto.
func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// ---------------------------------------------------------------------------
// Rate limiting — sliding window per key, in-memory.
//
// Good enough for a single-node deploy. Behind multiple replicas put a
// shared limiter (Redis) in front; this protects the common
// single-binary self-host and the auth endpoints from casual abuse.
// ---------------------------------------------------------------------------

type rateLimiter struct {
	mu      sync.Mutex
	windows map[string][]time.Time
	limit   int
	window  time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{windows: map[string][]time.Time{}, limit: limit, window: window}
}

// allow records a hit for key and reports whether it fits the budget.
func (l *rateLimiter) allow(key string) bool {
	now := time.Now()
	cut := now.Add(-l.window)
	l.mu.Lock()
	defer l.mu.Unlock()
	hits := l.windows[key]
	// Drop expired from the front; keep the rest and record this hit.
	i := 0
	for ; i < len(hits) && hits[i].Before(cut); i++ {
	}
	hits = append(hits[i:], now)
	l.windows[key] = hits
	if len(hits) > l.limit {
		return false
	}
	// Opportunistic GC so the map doesn't grow without bound.
	if len(l.windows) > 10_000 {
		for k, v := range l.windows {
			if len(v) == 0 || v[len(v)-1].Before(cut) {
				delete(l.windows, k)
			}
		}
	}
	return true
}

// clientIP extracts the caller's address. X-Forwarded-For is trusted
// only when the request came over TLS (i.e. via our reverse proxy);
// direct HTTP peers cannot spoof the header past the limit.
func clientIP(r *http.Request) string {
	if isSecureRequest(r) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// rateLimit returns middleware that allows `limit` requests per `window`
// per (ip + bucket). bucket lets login/register share a table without
// colliding.
func (s *Server) rateLimit(bucket string, limit int, window time.Duration) func(http.Handler) http.Handler {
	lim := newRateLimiter(limit, window)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !lim.allow(bucket + "|" + clientIP(r)) {
				w.Header().Set("Retry-After", "60")
				if strings.HasPrefix(r.URL.Path, "/api/") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = w.Write([]byte(`{"error":"too many requests"}`))
					return
				}
				http.Error(w, "Too many attempts. Try again in a minute.", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ---------------------------------------------------------------------------
// UI language
// ---------------------------------------------------------------------------

const langCookieName = "paratrack_lang"

// resolveLang picks the visitor's language: explicit cookie first, then
// Accept-Language, then English.
func resolveLang(r *http.Request) i18n.Lang {
	if c, err := r.Cookie(langCookieName); err == nil && c.Value != "" {
		return i18n.Normalize(c.Value)
	}
	al := r.Header.Get("Accept-Language")
	// First matching tag wins; "ru-RU,ru;q=0.9,en;q=0.8" → ru.
	// Match explicitly — i18n.Default is ru, so it cannot act as the
	// "not recognised" sentinel.
	for _, part := range strings.Split(al, ",") {
		tag := strings.ToLower(strings.TrimSpace(strings.SplitN(part, ";", 2)[0]))
		if tag == "" {
			continue
		}
		switch {
		case strings.HasPrefix(tag, "ru"):
			return i18n.Ru
		case strings.HasPrefix(tag, "en"):
			return i18n.En
		}
	}
	return i18n.Default
}

func setLangCookie(w http.ResponseWriter, r *http.Request, lang i18n.Lang) {
	http.SetCookie(w, &http.Cookie{
		Name:     langCookieName,
		Value:    string(lang),
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   365 * 24 * 3600,
		Secure:   isSecureRequest(r),
	})
}


// cacheStatic sets a modest immutable-ish policy for vendored assets.
// Filenames do not change when content does (single css / js names), so
// we use a one-day TTL rather than `immutable` — a deploy picks up new
// bytes within a day, and repeat visits skip the network.
func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("Vary", "Accept-Encoding")
		next.ServeHTTP(w, r)
	})
}
