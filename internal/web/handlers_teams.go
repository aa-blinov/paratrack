package web

import (
	"encoding/base64"
	"fmt"
	"html/template"
	"io"
	netmail "net/mail"
	"unicode"
	"net/url"

	"errors"
	"github.com/aa-blinov/paratrack/internal/mail"
	"net/http"
	"strconv"
	"strings"

	"github.com/aa-blinov/paratrack/internal/auth"
	dbpkg "github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/i18n"
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
	Pay     map[int64]memberPayView // members page: current pay + capacity
	Flash   string                  // success / error banner shown above the form
	FlashOK bool
	// Team settings: workspace currency and the menu of choices.
	Currency   string
	Currencies []currencyOption
	Requisites string
	VATNote    string
	Billing    dbpkg.BillingRules
	RoundOpts  []int
	LogoURL    template.URL // data: URL, set only from our own upload check
	IsOwner    bool // members page: only the owner changes roles
	CanManage  bool
	CSRFToken  string
	Lang       string
}

func (p *settingsPageData) setCSRF(t string) { p.CSRFToken = t }
func (p *settingsPageData) setLang(l string) { p.Lang = l }
func (p *settingsPageData) setManage(v bool) { p.CanManage = v }

// T translates a dictionary key for this page's language.
func (p settingsPageData) T(key string) string { return i18n.T(i18n.Lang(p.Lang), key) }

type memberPayView struct {
	Rate     string // currency units, "" when unset
	Capacity int
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
		Active: "settings-team",
		Team:   team,
		User:   userViewOf(user),
	}
	data.Currency, _ = s.db.TeamCurrency(r.Context(), team.ID)
	data.Currencies = currencyOptions()
	data.Requisites, data.VATNote, _ = s.db.TeamRequisites(r.Context(), team.ID)
	data.Billing, _ = s.db.TeamBilling(r.Context(), team.ID)
	data.RoundOpts = []int{0, 6, 15, 30, 60}
	data.LogoURL = logoURL(data.Billing.Logo)
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, "Team settings", "settings", "team-settings", &data)
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
		Active:  "settings-members",
		Team:    team,
		User:    userViewOf(user),
		Members: members,
		IsOwner: RoleFrom(r.Context()) == teams.RoleOwner,
		Pay:     map[int64]memberPayView{},
	}
	for _, m := range members {
		if cents, capMin, err := s.db.MemberPay(r.Context(), team.ID, m.UserID); err == nil {
			v := memberPayView{Capacity: capMin}
			if cents > 0 {
				v.Rate = formatMoneyInput(resolveLang(r), cents)
			}
			data.Pay[m.UserID] = v
		}
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, "Members", "settings", "team-members", &data)
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
		Active:  "settings-invites",
		Team:    team,
		User:    userViewOf(user),
		Invites: invites,
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, "Invites", "settings", "team-invites", &data)
}

func (s *Server) handleSettingsProfile(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	data := settingsPageData{
		Title:  "Profile",
		Active: "settings-profile",
		User:   userViewOf(user),
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, "Profile", "settings", "profile", &data)
}

// invitePage is the /invites/{token} envelope. T() exposes i18n.
type invitePage struct {
	Title     string
	Token     string
	Invite    teams.Invite
	Team      teams.Team
	User      teamUserView
	LoggedIn  bool
	CSRFToken string
	Lang      string
}

func (p invitePage) T(key string) string { return i18n.T(i18n.Lang(p.Lang), key) }

func (s *Server) handleInviteAcceptPage(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/invites/")
	user, authed := UserFrom(r.Context())
	data := invitePage{
		Title: "Join team", Token: token, LoggedIn: authed, User: userViewOf(user),
		CSRFToken: ensureCSRF(w, r), Lang: string(resolveLang(r)),
	}
	if inv, err := s.teams.FindInvite(r.Context(), token); err == nil {
		data.Invite = inv
		if t, err := s.teams.FindByID(r.Context(), inv.TeamID); err == nil {
			data.Team = t
		}
	}
	s.renderPageForRequest(w, r, "Join team", "", "invite-accept", &data)
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

// handleAPITeamCurrency sets the workspace currency. Documents already
// issued keep theirs; new projects without their own currency follow it.
func (s *Server) handleAPITeamCurrency(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	cur := r.FormValue("currency")
	if !validCurrency(cur) {
		http.Redirect(w, r, "/settings/team?flash=bad_request", http.StatusSeeOther)
		return
	}
	if err := s.db.SetTeamCurrency(r.Context(), team.ID, cur); err != nil {
		http.Redirect(w, r, "/settings/team?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	s.audit(r, "team.currency", cur, "")
	http.Redirect(w, r, "/settings/team?flash=updated", http.StatusSeeOther)
}

// handleAPITeamRequisites saves the issuer's details and VAT line printed
// on new invoices and acts.
func (s *Server) handleAPITeamRequisites(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	req := strings.TrimSpace(r.FormValue("requisites"))
	vat := strings.TrimSpace(r.FormValue("vat_note"))
	if len(req) > 2000 || len(vat) > 200 {
		http.Redirect(w, r, "/settings/team?flash=bad_request", http.StatusSeeOther)
		return
	}
	if err := s.db.SetTeamRequisites(r.Context(), team.ID, req, vat); err != nil {
		http.Redirect(w, r, "/settings/team?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/team?flash=updated", http.StatusSeeOther)
}

// handleAPIMemberRole makes a member an admin or back (owner only).
func (s *Server) handleAPIMemberRole(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	user, _ := UserFrom(r.Context())
	target, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/settings/members?flash=bad_request", http.StatusSeeOther)
		return
	}
	if err := s.teams.SetRole(r.Context(), team.ID, target, user.ID, teams.Role(r.FormValue("role"))); err != nil {
		http.Redirect(w, r, "/settings/members?flash=forbidden", http.StatusSeeOther)
		return
	}
	s.audit(r, "member.role", strconv.FormatInt(target, 10), r.FormValue("role"))
	http.Redirect(w, r, "/settings/members?flash=updated", http.StatusSeeOther)
}

func (s *Server) handleAPITeamDelete(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	team, _ := TeamFrom(r.Context())
	if err := s.teams.Delete(r.Context(), team.ID, user.ID); err != nil {
		http.Redirect(w, r, "/settings/team?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	// If the user still belongs to another workspace, switch to it and
	// stay signed in. Only a user with no teams left is logged out.
	if remaining, err := s.teams.ListForUser(r.Context(), user.ID); err == nil && len(remaining) > 0 {
		setTeamCookie(w, r, remaining[0].ID)
		http.Redirect(w, r, "/?flash=team_deleted", http.StatusSeeOther)
		return
	}
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
	if _, ok, err := s.teams.IsMember(r.Context(), id, user.ID); err != nil || !ok {
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

func (s *Server) handleAPIInviteCreate(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	user, _ := UserFrom(r.Context())
	inv, err := s.teams.NewInvite(r.Context(), team.ID, user.ID)
	if err != nil {
		http.Redirect(w, r, "/settings/invites?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	link := publicBaseURL(r) + "/invites/" + inv.Token
	// With an address, the invitation goes out as a letter too.
	if to := strings.TrimSpace(r.FormValue("email")); to != "" {
		if _, err := netmail.ParseAddress(to); err == nil {
			subj, ev := inviteEmail(resolveLang(r), user.Name, team.Name, link)
			if msg, err := s.buildEmail(to, subj, ev); err == nil && s.deliver(msg) == nil && mail.Configured() {
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
	// Their timers here stop now: nobody else may touch them, and they
	// can't reach them any more.
	sctx := dbpkg.WithScope(r.Context(), uid)
	if open, err := s.db.ListActiveSessions(sctx, team.ID); err == nil {
		for _, as := range open {
			_, _ = s.db.UpdateSessionEnd(sctx, team.ID, as.Session.ID, userNow(r))
		}
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
	setTeamCookie(w, r, team.ID)
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
	// Current password is mandatory — a live session alone must not be
	// enough to take over the account.
	stored, err := s.auth.FindByID(r.Context(), user.ID)
	if err != nil || s.auth.VerifyPassword(stored, curPw) != nil {
		http.Redirect(w, r, "/settings/profile?flash="+encodeFlash(false, "current password is wrong"), http.StatusSeeOther)
		return
	}
	if err := s.auth.UpdatePassword(r.Context(), user.ID, newPw); err != nil {
		http.Redirect(w, r, "/settings/profile?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/profile?flash=password_updated", http.StatusSeeOther)
}

// ----- helpers -----------------------------------------------------

func userViewOf(u auth.User) teamUserView {
	return teamUserView{ID: u.ID, Email: u.Email, Name: u.Name}
}

// decodeFlash maps a flash code into (message, ok). Errors pass
// through verbatim, named codes map to friendly strings.
func decodeFlash(code string, lang i18n.Lang) (string, bool) {
	switch code {
	case "renamed":
		return i18n.T(lang, "flash.renamed"), true
	case "created":
		return i18n.T(lang, "flash.created"), true
	case "revoked":
		return i18n.T(lang, "flash.revoked"), true
	case "removed":
		return i18n.T(lang, "flash.removed"), true
	case "updated":
		return i18n.T(lang, "flash.updated"), true
	case "password_updated":
		return i18n.T(lang, "flash.passwordUpdated"), true
	case "joined":
		return i18n.T(lang, "flash.joined"), true
	case "team_deleted":
		return i18n.T(lang, "flash.teamDeleted"), true
	case "bad_request":
		return i18n.T(lang, "flash.badRequest"), false
	case "forbidden":
		return i18n.T(lang, "flash.forbidden"), false
	case "bad_team":
		return i18n.T(lang, "flash.badTeam"), false
	case "already_billed":
		return i18n.T(lang, "flash.alreadyBilled"), false
	case "logo_big":
		return i18n.T(lang, "flash.logoBig"), false
	case "logo_type":
		return i18n.T(lang, "flash.logoType"), false
	default:
		msg, ok := strings.CutPrefix(code, "e:")
		// Plain-text flashes ("bad period", "marked paid") translate via
		// flash.msg.<text>; anything else (a db error) shows as is.
		if t := i18n.T(lang, "flash.msg."+msg); t != "flash.msg."+msg {
			msg = t
		}
		return msg, !ok
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

// invoicePrefixOK: short, letters/digits (any script), dashes inside.
func invoicePrefixOK(p string) bool {
	if p == "" || len([]rune(p)) > 12 {
		return false
	}
	for _, c := range p {
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '-' {
			return false
		}
	}
	return true
}

// handleAPITeamBilling saves rounding and the invoice number prefix.
func (s *Server) handleAPITeamBilling(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	b, _ := s.db.TeamBilling(r.Context(), team.ID)
	b.RoundMinutes, _ = strconv.Atoi(r.FormValue("round_minutes"))
	switch b.RoundMinutes {
	case 0, 6, 15, 30, 60:
	default:
		http.Redirect(w, r, "/settings/team?flash=bad_request", http.StatusSeeOther)
		return
	}
	b.RoundMode = "nearest"
	if r.FormValue("round_mode") == "up" {
		b.RoundMode = "up"
	}
	b.InvoicePrefix = strings.TrimSpace(r.FormValue("invoice_prefix"))
	if !invoicePrefixOK(b.InvoicePrefix) {
		http.Redirect(w, r, "/settings/team?flash=bad_request", http.StatusSeeOther)
		return
	}
	if err := s.db.SetTeamBilling(r.Context(), team.ID, b); err != nil {
		http.Redirect(w, r, "/settings/team?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	s.audit(r, "team.billing", fmt.Sprintf("%d %s %s", b.RoundMinutes, b.RoundMode, b.InvoicePrefix), "")
	http.Redirect(w, r, "/settings/team?flash=updated#billing", http.StatusSeeOther)
}

// handleAPITeamLogo stores a small PNG/JPEG as a data URL for invoices
// and acts; "remove" clears it.
func (s *Server) handleAPITeamLogo(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	const max = 200 << 10
	if err := r.ParseMultipartForm(2 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		http.Redirect(w, r, "/settings/team?flash=logo_big#billing", http.StatusSeeOther)
		return
	}
	logo := ""
	if r.FormValue("remove") == "" {
		f, _, err := r.FormFile("logo")
		if err != nil {
			http.Redirect(w, r, "/settings/team?flash=bad_request#billing", http.StatusSeeOther)
			return
		}
		defer f.Close()
		raw, _ := io.ReadAll(io.LimitReader(f, max+1))
		if len(raw) > max {
			http.Redirect(w, r, "/settings/team?flash=logo_big#billing", http.StatusSeeOther)
			return
		}
		ct := http.DetectContentType(raw)
		if ct != "image/png" && ct != "image/jpeg" {
			http.Redirect(w, r, "/settings/team?flash=logo_type#billing", http.StatusSeeOther)
			return
		}
		logo = "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(raw)
	}
	if err := s.db.SetTeamLogo(r.Context(), team.ID, logo); err != nil {
		http.Redirect(w, r, "/settings/team?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/team?flash=updated#billing", http.StatusSeeOther)
}

// logoURL lets a stored logo into an <img src>; only the two data: forms
// the upload handler writes pass.
func logoURL(v string) template.URL {
	if strings.HasPrefix(v, "data:image/png;base64,") || strings.HasPrefix(v, "data:image/jpeg;base64,") {
		return template.URL(v)
	}
	return ""
}
