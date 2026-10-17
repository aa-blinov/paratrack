package web

import (
	"errors"
	"net/http"
	"strings"
)

// A workspace that has not picked its sections waits at /welcome before any
// screen, so a new account never lands in a menu it did not choose. The screens
// that stay open are the ones a stranded account needs: the choice itself,
// settings (profile, password, sign-out, the sections page), help and the
// invite page. Nobody is locked out of the way to fix it.
func sectionsGateOpen(path string) bool {
	switch {
	case path == "/welcome", path == "/help", path == "/settings", path == "/invites",
		strings.HasPrefix(path, "/settings/"), strings.HasPrefix(path, "/invites/"):
		return true
	}
	return false
}

// requireSectionsChosen runs after requireAuth, so the workspace is already in
// the request context.
func (s *Server) requireSectionsChosen(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sectionsGateOpen(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		team, ok := TeamFrom(r.Context())
		if !ok || team.ID <= 0 {
			s.writeInternalError(w, errors.New("sections gate reached without a workspace"))
			return
		}
		chosen, err := s.services.Teams.Settings.SectionsChosen(r.Context(), team.ID)
		if err != nil {
			s.writeInternalError(w, err)
			return
		}
		if !chosen {
			http.Redirect(w, r, "/welcome", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}
