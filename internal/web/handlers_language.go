package web

import (
	"net/http"
	"strings"

	"github.com/aa-blinov/paratrack/internal/i18n"
)

func (s *Server) handleSetLang(w http.ResponseWriter, r *http.Request) {
	code := i18n.Normalize(r.PathValue("code"))
	next := r.URL.Query().Get("next")
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = "/"
	}
	// Already on this language — do not touch the cookie or force a
	// needless round-trip. (The UI also marks the current language as
	// non-clickable; this covers deep links and old bookmarks.)
	if resolveLang(r) == code {
		if c, err := r.Cookie(langCookieName); err == nil && c.Value == string(code) {
			http.Redirect(w, r, next, http.StatusSeeOther)
			return
		}
	}
	setLangCookie(w, r, code)
	http.Redirect(w, r, next, http.StatusSeeOther)
}
