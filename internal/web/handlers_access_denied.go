package web

import (
	"net/http"

	"github.com/aa-blinov/paratrack/internal/i18n"
)

// accessDeniedPage is the 403 screen for a manager-only page. It is a React
// screen like every other page: the server only says why access is refused.
type accessDeniedPage struct {
	pageData
	AccessDeniedReact bool
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
