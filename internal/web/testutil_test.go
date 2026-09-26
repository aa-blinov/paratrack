package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// seedCSRF performs a GET /login so the CSRF middleware issues its
// double-submit cookie, and returns the raw token plus a cookie ready
// to attach to a mutating request.
func seedCSRF(t *testing.T, h http.Handler) (token string, cookie *http.Cookie) {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/login", nil)
	h.ServeHTTP(w, r)
	for _, c := range w.Result().Cookies() {
		if c.Name == csrfCookieName {
			return c.Value, c
		}
	}
	t.Fatal("GET /login did not issue a paratrack_csrf cookie")
	return "", nil
}

// csrfRequest builds a form-encoded request that satisfies the
// double-submit CSRF check (cookie + header).
func csrfRequest(method, path, body, token string, cookies ...*http.Cookie) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set(csrfHeaderName, token)
	r.AddCookie(&http.Cookie{Name: csrfCookieName, Value: token})
	for _, c := range cookies {
		r.AddCookie(c)
	}
	return r
}
