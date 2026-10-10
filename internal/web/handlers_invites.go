package web

import (
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/mailport"
	"net/http"
	netmail "net/mail"
	"net/url"
	"strings"
)

func (s *Server) handleAPIInviteCreate(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	user, _ := UserFrom(r.Context())
	inv, err := s.services.Teams.Invitations.NewInvite(r.Context(), appmodel.TeamInviteCreateRequest{TeamID: team.ID, CallerID: user.ID})
	if err != nil {
		http.Redirect(w, r, "/settings/invites?flash="+encodeFlash(false, s.teamErrorMessage(r, err)), http.StatusSeeOther)
		return
	}
	lang := resolveLang(r)
	link := s.publicBaseURL(r) + "/invites/" + inv.Token
	// With an address, the invitation goes out as a letter too.
	if to := strings.TrimSpace(r.FormValue("email")); to != "" {
		if _, err := netmail.ParseAddress(to); err == nil && mailport.Available(s.runtime.Mailer) {
			subj, ev := inviteEmail(lang, user.Name, team.Name, link)
			if msg, err := s.buildEmail(r, to, subj, ev); err == nil && s.deliverPostcommit(r.Context(), msg) == nil {
				redirectWithFreshInvite(w, r, i18n.T(lang, "flash.inviteMailed")+" "+to+".", link)
				return
			}
		}
	}
	redirectWithFreshInvite(w, r, i18n.T(lang, "flash.inviteCreated"), link)
}

// redirectWithFreshInvite sends the owner back to the invitations page with
// the new link in its own parameter. The page needs the URL as a value of its
// own to offer a copy button; leaving it inside the flash sentence would keep
// it unselectable and uncopyable.
func redirectWithFreshInvite(w http.ResponseWriter, r *http.Request, message, link string) {
	query := url.Values{"flash": {encodeFlash(true, message)}, "invite": {link}}
	http.Redirect(w, r, "/settings/invites?"+query.Encode(), http.StatusSeeOther)
}

func (s *Server) handleAPIInviteRevoke(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	user, _ := UserFrom(r.Context())
	token := strings.TrimPrefix(r.URL.Path, "/api/team/invites/")
	token = strings.TrimSuffix(token, "/revoke")
	// Look up the invitation first: dropping a spent row is cleanup, and the
	// message must not read as if somebody's access had been taken back. A
	// failed lookup is not fatal here — the revoke below reports its own error.
	spent := false
	if invite, err := s.services.Teams.Invitations.FindInvite(r.Context(), token); err == nil {
		spent = invite.Used()
	}
	if err := s.services.Teams.Invitations.RevokeInvite(r.Context(), appmodel.TeamInviteRevokeRequest{TeamID: team.ID, CallerID: user.ID, Token: token}); err != nil {
		http.Redirect(w, r, "/settings/invites?flash="+encodeFlash(false, s.teamErrorMessage(r, err)), http.StatusSeeOther)
		return
	}
	flash := "revoked"
	if spent {
		flash = encodeFlash(true, i18n.T(resolveLang(r), "flash.inviteRemoved"))
	}
	http.Redirect(w, r, "/settings/invites?flash="+flash, http.StatusSeeOther)
}

func (s *Server) handleAPIInviteAccept(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFrom(r.Context())
	if !ok {
		// Shouldn't happen because middleware blocks /invites/*, but
		// just in case, bounce to /login with the token preserved.
		http.Redirect(w, r, "/login?next=/invites/"+strings.TrimPrefix(r.URL.Path, "/api/invites/"), http.StatusSeeOther)
		return
	}
	token := strings.TrimPrefix(r.URL.Path, "/api/invites/")
	token = strings.TrimSuffix(token, "/accept")
	team, err := s.services.Teams.Invitations.AcceptInvite(r.Context(), appmodel.TeamInviteAcceptRequest{Token: token, UserID: user.ID})
	if err != nil {
		http.Redirect(w, r, "/invites/"+token+"?flash="+encodeFlash(false, s.teamErrorMessage(r, err)), http.StatusSeeOther)
		return
	}
	setTeamCookie(w, r, team.ID)
	// Joining a studio is where someone starts working in it, so it
	// becomes the space a later login opens.
	s.rememberWorkspace(r, user.ID, team.ID)
	http.Redirect(w, r, "/?flash=joined", http.StatusSeeOther)
}

// ----- helpers -----------------------------------------------------
