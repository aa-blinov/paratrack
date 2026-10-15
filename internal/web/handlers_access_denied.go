package web

import (
	"net/http"
	"strings"

	"github.com/aa-blinov/paratrack/internal/i18n"
)

// accessDeniedPage is the 403 screen for a manager-only page. It is a React
// screen like every other page: the server only says why access is refused.
type accessDeniedPage struct {
	pageData
	AccessDeniedReact bool
	Heading           string
	Message           string
}

func (s *Server) renderAccessDenied(w http.ResponseWriter, r *http.Request) {
	lang := resolveLang(r)
	data := accessDeniedPage{
		pageData:          pageData{Title: "Access denied", Lang: string(lang), ReactApp: true},
		AccessDeniedReact: true,
		Message:           i18n.T(lang, "err.managersOnly"),
	}
	s.renderPageForRequestStatus(w, r, http.StatusForbidden, "Access denied", "", &data)
}

// shellNotFound turns the plain-text 404 a page handler writes (http.NotFound,
// http.Error, or no route at all) into the app's own screen, so /invoices/new
// is not a bare "404 page not found" outside the menu. Only a browser asking
// for a page gets it: HTMX swaps and JSON callers keep the status as it was.
func (s *Server) shellNotFound(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("HX-Request") != "" || !strings.Contains(r.Header.Get("Accept"), "text/html") {
			next.ServeHTTP(w, r)
			return
		}
		nf := &notFoundWriter{ResponseWriter: w}
		next.ServeHTTP(nf, r)
		if !nf.caught {
			return
		}
		w.Header().Del("Content-Type")
		w.Header().Del("X-Content-Type-Options")
		lang := resolveLang(r)
		data := accessDeniedPage{
			pageData:          pageData{Title: "Not found", Lang: string(lang), ReactApp: true},
			AccessDeniedReact: true,
			Heading:           i18n.T(lang, "err.notFoundTitle"),
			Message:           i18n.T(lang, "err.notFoundHint"),
		}
		s.renderPageForRequestStatus(w, r, http.StatusNotFound, "Not found", "", &data)
	})
}

type notFoundWriter struct {
	http.ResponseWriter
	caught bool
}

func (w *notFoundWriter) WriteHeader(code int) {
	if code == http.StatusNotFound && strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
		w.caught = true
		return
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *notFoundWriter) Write(b []byte) (int, error) {
	if w.caught {
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}

func (w *notFoundWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
