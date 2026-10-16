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
	// SignIn: a visitor without an account of their own; the screen offers sign-in.
	SignIn bool
}

// handleAccessDenied serves /access-denied itself, for a bookmark or a link to
// a manager-only screen. The same screen, with the generic heading, and no
// reason beyond the one every manager-only page gives.
func (s *Server) handleAccessDenied(w http.ResponseWriter, r *http.Request) {
	lang := resolveLang(r)
	data := accessDeniedPage{
		pageData:          pageData{Title: "Access denied", Lang: string(lang), ReactApp: true},
		AccessDeniedReact: true,
		Heading:           i18n.T(lang, "err.accessDeniedTitle"),
		Message:           i18n.T(lang, "err.managersOnly"),
	}
	s.renderPageForRequestStatus(w, r, http.StatusForbidden, "Access denied", "", &data)
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

// pageAuthFailure answers a visitor who is not signed in. An address a page
// answers goes to the sign-in form; an address no page answers gets the
// not-found screen, because the sign-in form is for a page, not for a typo.
func (s *Server) pageAuthFailure(pageRoutes *http.ServeMux) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := pageRoutes.Handler(r); pattern == "" && r.Method == http.MethodGet {
			s.renderUnknownPage(w, r)
			return
		}
		pageRedirect(w, r)
	}
}

// renderUnknownPage is the not-found screen for an address nothing serves,
// shown to a visitor who is not signed in: no menu, so the page offers the way
// in instead of the way back to a menu they do not have.
func (s *Server) renderUnknownPage(w http.ResponseWriter, r *http.Request) {
	lang := resolveLang(r)
	data := accessDeniedPage{
		pageData:          pageData{Title: "Not found", Lang: string(lang), ReactApp: true},
		AccessDeniedReact: true,
		Heading:           i18n.T(lang, "err.notFoundTitle"),
		Message:           i18n.T(lang, "err.notFoundHintGuest"),
		SignIn:            true,
	}
	s.renderPageStatus(w, r, http.StatusNotFound, "Not found", "", &data, false)
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
