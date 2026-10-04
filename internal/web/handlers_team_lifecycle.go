package web

import (
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) handleAPITeamDelete(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	team, _ := TeamFrom(r.Context())
	remaining, err := s.services.TeamOps.DeleteWorkspace(r.Context(), appmodel.WorkspaceDeleteRequest{TeamID: team.ID, CallerID: user.ID})
	if err != nil {
		http.Redirect(w, r, "/settings/team?flash="+encodeFlash(false, s.teamErrorMessage(r, err)), http.StatusSeeOther)
		return
	}
	// If the user still belongs to another workspace, switch to it and
	// stay signed in. Only a user with no teams left is logged out.
	if len(remaining) > 0 {
		setTeamCookie(w, r, remaining[0])
		http.Redirect(w, r, "/?flash=team_deleted", http.StatusSeeOther)
		return
	}
	clearSessionCookie(w)
	http.Redirect(w, r, "/register?flash=team_deleted", http.StatusSeeOther)
}

func (s *Server) handleAPITeamCreate(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/team?flash=bad_request", http.StatusSeeOther)
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	team, err := s.services.Teams.Administration.Create(r.Context(), appmodel.TeamCreateRequest{OwnerID: user.ID, Name: name})
	if err != nil {
		http.Redirect(w, r, "/settings/team?flash="+encodeFlash(false, s.teamErrorMessage(r, err)), http.StatusSeeOther)
		return
	}
	setTeamCookie(w, r, team.ID)
	// A new workspace picks its sections like a new account does.
	http.Redirect(w, r, "/welcome", http.StatusSeeOther)
}

func (s *Server) handleAPITeamSwitch(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/?flash=bad_request", http.StatusSeeOther)
		return
	}
	idStr := r.PostForm.Get("team_id")
	id, err := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/?flash=bad_team", http.StatusSeeOther)
		return
	}
	if _, ok, err := s.services.Teams.Directory.IsMember(r.Context(), appmodel.TeamMembershipQuery{TeamID: id, UserID: user.ID}); err != nil || !ok {
		http.Redirect(w, r, "/?flash=forbidden", http.StatusSeeOther)
		return
	}
	setTeamCookie(w, r, id)
	next := strings.TrimSpace(r.PostForm.Get("next"))
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}
