package web

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/aa-blinov/paratrack/internal/i18n"
)

func (s *Server) handleSetLang(w http.ResponseWriter, r *http.Request) {
	code := i18n.Normalize(r.PathValue("code"))
	next := r.URL.Query().Get("next")
	if !localPath(next) {
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

// localPath reports whether next is a path on this site, safe to redirect to
// after sign-in or a switch. "//host" and "/\host" both leave the site in a
// browser (the WHATWG parser reads a backslash as a slash), so neither passes;
// nor does anything with a scheme, a host or a control character.
func localPath(next string) bool {
	if next == "" || next[0] != '/' || strings.ContainsAny(next, "\\\r\n\t") || strings.HasPrefix(next, "//") {
		return false
	}
	u, err := url.Parse(next)
	return err == nil && u.Scheme == "" && u.Host == ""
}
