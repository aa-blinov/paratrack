package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/aa-blinov/paratrack/internal/auth"
	"github.com/aa-blinov/paratrack/internal/teams"
)

// settingsPageData is the common envelope for /settings/* pages.
type settingsPageData struct {
	Title  string
	Active string
	Team   teams.Team
	User   teamUserView
	// Members + invites populated by their respective handlers.
	Members []teams.Member
	Invites []teams.Invite
	Flash   string // success / error banner shown above the form
	FlashOK bool
}

// teamUserView is the subset of User we render in templates. Kept
// separate so we don't drag json tags into HTML rendering.
type teamUserView struct {
	ID    int64
	Email string
	Name  string
}

func (s *Server) handleTeamSettings(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	user, _ := UserFrom(r.Context())
	data := settingsPageData{
		Title:  "Team settings",
		Active: "settings",
		Team:   team,
		User:   userViewOf(user),
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash)
	}
	s.renderPageForRequest(w, r, "Team settings", "settings", "team-settings", data)
}

func (s *Server) handleTeamMembers(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	user, _ := UserFrom(r.Context())
	members, err := s.teams.Members(r.Context(), team.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data := settingsPageData{
		Title:   "Members",
		Active:  "settings",
		Team:    team,
		User:    userViewOf(user),
		Members: members,
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash)
	}
	s.renderPageForRequest(w, r, "Members", "settings", "team-members", data)
}

func (s *Server) handleTeamInvites(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	user, _ := UserFrom(r.Context())
	invites, err := s.teams.InvitesForTeam(r.Context(), team.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data := settingsPageData{
		Title:   "Invites",
		Active:  "settings",
		Team:    team,
		User:    userViewOf(user),
		Invites: invites,
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash)
	}
	s.renderPageForRequest(w, r, "Invites", "settings", "team-invites", data)
}

func (s *Server) handleSettingsProfile(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	data := settingsPageData{
		Title:  "Profile",
		Active: "settings",
		User:   userViewOf(user),
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash)
	}
	s.renderPageForRequest(w, r, "Profile", "settings", "profile", data)
}

func (s *Server) handleInviteAcceptPage(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/invites/")
	user, authed := UserFrom(r.Context())
	data := struct {
		Title    string
		Token    string
		Invite   teams.Invite
		Team     teams.Team
		User     teamUserView
		LoggedIn bool
	}{Title: "Join team", Token: token, LoggedIn: authed, User: userViewOf(user)}
	if inv, err := s.teams.FindInvite(r.Context(), token); err == nil {
		data.Invite = inv
		if t, err := s.teams.FindByID(r.Context(), inv.TeamID); err == nil {
			data.Team = t
		}
	}
	s.renderPageForRequest(w, r, "Join team", "", "invite-accept", data)
}

// ----- POST handlers (settings actions) ----------------------------

func (s *Server) handleAPITeamRename(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/team?flash=bad_request", http.StatusSeeOther)
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	if err := s.teams.Rename(r.Context(), team.ID, name); err != nil {
		http.Redirect(w, r, "/settings/team?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/team?flash=renamed", http.StatusSeeOther)
}

func (s *Server) handleAPITeamDelete(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	team, _ := TeamFrom(r.Context())
	if err := s.teams.Delete(r.Context(), team.ID, user.ID); err != nil {
		http.Redirect(w, r, "/settings/team?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	// User has no teams left → log them out and bounce to /register.
	_ = s.auth.DeleteByUser(r.Context(), user.ID)
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
	team, err := s.teams.Create(r.Context(), user.ID, name)
	if err != nil {
		http.Redirect(w, r, "/settings/team?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	setTeamCookie(w, team.ID)
	http.Redirect(w, r, "/settings/team?flash=created", http.StatusSeeOther)
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
	if _, ok, err := s.teams.IsMember(r.Context(), id, user.ID); err != nil || !ok {
		http.Redirect(w, r, "/?flash=forbidden", http.StatusSeeOther)
		return
	}
	setTeamCookie(w, id)
	http.Redirect(w, r, r.PostForm.Get("next"), http.StatusSeeOther)
}

func (s *Server) handleAPIInviteCreate(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	user, _ := UserFrom(r.Context())
	inv, err := s.teams.NewInvite(r.Context(), team.ID, user.ID)
	if err != nil {
		http.Redirect(w, r, "/settings/invites?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/invites?flash="+encodeFlash(true, "Invite created: "+inv.Token), http.StatusSeeOther)
}

func (s *Server) handleAPIInviteRevoke(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	user, _ := UserFrom(r.Context())
	token := strings.TrimPrefix(r.URL.Path, "/api/team/invites/")
	if err := s.teams.RevokeInvite(r.Context(), team.ID, user.ID, token); err != nil {
		http.Redirect(w, r, "/settings/invites?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/invites?flash=revoked", http.StatusSeeOther)
}

func (s *Server) handleAPIMemberRemove(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	caller, _ := UserFrom(r.Context())
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/team/members/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "missing user id", http.StatusBadRequest)
		return
	}
	uid, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.Error(w, "bad user id", http.StatusBadRequest)
		return
	}
	if err := s.teams.RemoveMember(r.Context(), team.ID, uid, caller.ID); err != nil {
		http.Redirect(w, r, "/settings/members?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/members?flash=removed", http.StatusSeeOther)
}

func (s *Server) handleAPIInviteAccept(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	if _, ok := UserFrom(r.Context()); !ok {
		// Shouldn't happen because middleware blocks /invites/*, but
		// just in case, bounce to /login with the token preserved.
		http.Redirect(w, r, "/login?next=/invites/"+strings.TrimPrefix(r.URL.Path, "/api/invites/"), http.StatusSeeOther)
		return
	}
	token := strings.TrimPrefix(r.URL.Path, "/api/invites/")
	token = strings.TrimSuffix(token, "/accept")
	team, err := s.teams.AcceptInvite(r.Context(), token, user.ID)
	if err != nil {
		if errors.Is(err, teams.ErrValidation) {
			http.Redirect(w, r, "/invites/"+token+"?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/invites/"+token+"?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	setTeamCookie(w, team.ID)
	http.Redirect(w, r, "/?flash=joined", http.StatusSeeOther)
}

func (s *Server) handleAPIProfileUpdate(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/profile?flash=bad_request", http.StatusSeeOther)
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	if err := s.auth.UpdateName(r.Context(), user.ID, name); err != nil {
		http.Redirect(w, r, "/settings/profile?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/profile?flash=updated", http.StatusSeeOther)
}

func (s *Server) handleAPIProfilePassword(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/profile?flash=bad_request", http.StatusSeeOther)
		return
	}
	newPw := r.PostForm.Get("new_password")
	curPw := r.PostForm.Get("current_password")
	if curPw != "" {
		// Verify current password before allowing the change.
		stored, err := s.auth.FindByID(r.Context(), user.ID)
		if err != nil || s.auth.VerifyPassword(stored, curPw) != nil {
			http.Redirect(w, r, "/settings/profile?flash="+encodeFlash(false, "current password is wrong"), http.StatusSeeOther)
			return
		}
	}
	if err := s.auth.UpdatePassword(r.Context(), user.ID, newPw); err != nil {
		http.Redirect(w, r, "/settings/profile?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/profile?flash=password_updated", http.StatusSeeOther)
}

// ----- helpers -----------------------------------------------------

func userViewOf(u auth.User) teamUserView { return teamUserView{ID: u.ID, Email: u.Email, Name: u.Name} }

// decodeFlash maps a flash code into (message, ok). Errors pass
// through verbatim, named codes map to friendly strings.
func decodeFlash(code string) (string, bool) {
	switch code {
	case "renamed":
		return "Team renamed.", true
	case "created":
		return "Team created.", true
	case "revoked":
		return "Invite revoked.", true
	case "removed":
		return "Member removed.", true
	case "updated":
		return "Profile saved.", true
	case "password_updated":
		return "Password updated.", true
	case "joined":
		return "You joined the team.", true
	case "team_deleted":
		return "Team deleted. Sign up to start fresh.", true
	case "bad_request":
		return "Invalid form submission.", false
	case "forbidden":
		return "You don't have access to that team.", false
	case "bad_team":
		return "That team doesn't exist (or you're not in it).", false
	default:
		// Encoded error: prefix "e
		if strings.HasPrefix(code, "e:") {
			return strings.TrimPrefix(code, "e:"), false
		}
		return code, true
	}
}

// encodeFlash builds the URL-safe flash value. The "e:" prefix marks an
// error message coming straight from the layer.
func encodeFlash(ok bool, msg string) string {
	if ok {
		return msg
	}
	return "e:" + msg
}