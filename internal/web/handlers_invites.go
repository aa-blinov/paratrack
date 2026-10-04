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
	link := s.publicBaseURL(r) + "/invites/" + inv.Token
	// With an address, the invitation goes out as a letter too.
	if to := strings.TrimSpace(r.FormValue("email")); to != "" {
		if _, err := netmail.ParseAddress(to); err == nil && mailport.Available(s.runtime.Mailer) {
			subj, ev := inviteEmail(resolveLang(r), user.Name, team.Name, link)
			if msg, err := s.buildEmail(to, subj, ev); err == nil && s.deliverPostcommit(r.Context(), msg) == nil {
				http.Redirect(w, r, "/settings/invites?flash="+url.QueryEscape(encodeFlash(true,
					i18n.T(resolveLang(r), "flash.inviteMailed")+" "+to+". "+link)), http.StatusSeeOther)
				return
			}
		}
	}
	http.Redirect(w, r, "/settings/invites?flash="+url.QueryEscape(encodeFlash(true, i18n.T(resolveLang(r), "flash.inviteCreated")+" "+link)), http.StatusSeeOther)
}

func (s *Server) handleAPIInviteRevoke(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	user, _ := UserFrom(r.Context())
	token := strings.TrimPrefix(r.URL.Path, "/api/team/invites/")
	token = strings.TrimSuffix(token, "/revoke")
	if err := s.services.Teams.Invitations.RevokeInvite(r.Context(), appmodel.TeamInviteRevokeRequest{TeamID: team.ID, CallerID: user.ID, Token: token}); err != nil {
		http.Redirect(w, r, "/settings/invites?flash="+encodeFlash(false, s.teamErrorMessage(r, err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/invites?flash=revoked", http.StatusSeeOther)
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
	http.Redirect(w, r, "/?flash=joined", http.StatusSeeOther)
}

// ----- helpers -----------------------------------------------------
