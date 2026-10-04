package web

import (
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// settingsPageData is the common envelope for /settings/* pages.
type settingsPageData struct {
	Title             string
	Active            string
	TeamSettingsReact bool
	Team              teamView
	User              userView
	// Members + invites populated by their respective handlers.
	Members []memberView
	Invites []inviteView
	Pay     map[int64]memberPayView // members page: current pay + capacity
	Flash   string                  // success / error banner shown above the form
	FlashOK bool
	// Team settings: workspace currency and the menu of choices.
	Currency   string
	Currencies []currencyOption
	Requisites string
	VATNote    string
	Billing    billingRulesView
	RoundOpts  []int
	LogoURL    template.URL // data: URL, set only from our own upload check
	IsOwner    bool         // members page: only the owner changes roles
	CanManage  bool
	CSRFToken  string
	Lang       string
}

func (settingsPageData) isTemplateData() {}

func (p settingsPageData) usesReactApp() bool { return p.TeamSettingsReact }

func (p *settingsPageData) setCSRF(t string) { p.CSRFToken = t }
func (p *settingsPageData) setLang(l string) { p.Lang = l }
func (p *settingsPageData) setManage(v bool) { p.CanManage = v }

// T translates a dictionary key for this page's language.
func (p settingsPageData) T(key string) string { return i18n.T(i18n.Lang(p.Lang), key) }

type memberPayView struct {
	Rate     string // currency units, "" when unset
	Capacity int
}

type memberView struct {
	UserID int64
	Email  string
	Name   string
	Role   string
}

func memberViews(members []model.TeamMember) []memberView {
	views := make([]memberView, 0, len(members))
	for _, member := range members {
		views = append(views, memberView{
			UserID: member.UserID, Email: member.Email,
			Name: member.Name, Role: string(member.Role),
		})
	}
	return views
}

type billingRulesView struct {
	RoundMinutes  int
	RoundMode     string
	InvoicePrefix string
	HasLogo       bool
}

type inviteView struct {
	Token     string
	CreatedAt time.Time
	ExpiresAt time.Time
	Used      bool
	Expired   bool
	Live      bool
}

func inviteViews(invites []appmodel.TeamInviteResult, now time.Time) []inviteView {
	views := make([]inviteView, 0, len(invites))
	for _, invite := range invites {
		views = append(views, inviteViewOf(invite, now))
	}
	return views
}

func inviteViewOf(invite appmodel.TeamInviteResult, now time.Time) inviteView {
	used := invite.Used()
	expired := !used && invite.ExpiredAt(now)
	return inviteView{
		Token: invite.Token, CreatedAt: invite.CreatedAt, ExpiresAt: invite.ExpiresAt,
		Used: used, Expired: expired, Live: !used && !expired,
	}
}

func (s *Server) handleTeamSettings(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	user, _ := UserFrom(r.Context())
	data := settingsPageData{
		Title:             "Team settings",
		Active:            "settings-team",
		TeamSettingsReact: true,
		Team:              teamView{ID: team.ID, Name: team.Name, CreatedAt: team.CreatedAt},
		User:              userViewOf(user),
	}
	settings, err := s.services.Teams.Settings.Settings(r.Context(), team.ID)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	data.Currency = settings.Currency
	data.Currencies = currencyOptions()
	data.Requisites, data.VATNote = settings.Requisites, settings.VATNote
	data.Billing = billingRulesView{
		RoundMinutes:  settings.Billing.RoundMinutes,
		RoundMode:     settings.Billing.RoundMode,
		InvoicePrefix: settings.Billing.InvoicePrefix,
		HasLogo:       settings.Billing.Logo != "",
	}
	data.RoundOpts = []int{0, 6, 15, 30, 60}
	data.LogoURL = logoURL(settings.Billing.Logo)
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, "Team settings", "settings", "team-settings", &data)
}

func (s *Server) handleTeamMembers(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	user, _ := UserFrom(r.Context())
	snapshot, err := s.services.MemberAdmin.Management(r.Context(), team.ID)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	data := settingsPageData{
		Title:   "Members",
		Active:  "settings-members",
		Team:    teamView{ID: team.ID, Name: team.Name, CreatedAt: team.CreatedAt},
		User:    userViewOf(user),
		Members: memberViews(snapshot.Members),
		IsOwner: RoleFrom(r.Context()) == model.TeamRoleOwner,
		Pay:     map[int64]memberPayView{},
	}
	for _, settings := range snapshot.PaySettings {
		view := memberPayView{Capacity: settings.CapacityMinutes}
		if settings.PayCents > 0 {
			view.Rate = formatMoneyInput(resolveLang(r), settings.PayCents)
		}
		data.Pay[settings.UserID] = view
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, "Members", "settings", "team-members", &data)
}

func (s *Server) handleTeamInvites(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	user, _ := UserFrom(r.Context())
	invites, err := s.services.Teams.Invitations.InvitesForTeam(r.Context(), team.ID)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	data := settingsPageData{
		Title:   "Invites",
		Active:  "settings-invites",
		Team:    teamView{ID: team.ID, Name: team.Name, CreatedAt: team.CreatedAt},
		User:    userViewOf(user),
		Invites: inviteViews(invites, userNow(r)),
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, "Invites", "settings", "team-invites", &data)
}

// invitePage is the /invites/{token} envelope. T() exposes i18n.
type invitePage struct {
	Title     string
	Token     string
	Invite    inviteView
	Team      teamView
	User      userView
	LoggedIn  bool
	CSRFToken string
	Lang      string
}

func (invitePage) isTemplateData() {}

func (p invitePage) T(key string) string { return i18n.T(i18n.Lang(p.Lang), key) }

func (p *invitePage) setCSRF(token string) { p.CSRFToken = token }
func (p *invitePage) setLang(lang string)  { p.Lang = lang }

func (s *Server) handleInviteAcceptPage(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/invites/")
	user, authed := UserFrom(r.Context())
	snapshot, err := s.services.Teams.Invitations.InvitePage(r.Context(), token)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	data := invitePage{
		Title: "Join team", Token: token, Invite: inviteViewOf(snapshot.Invite, userNow(r)),
		Team:     teamView{ID: snapshot.Team.ID, Name: snapshot.Team.Name, CreatedAt: snapshot.Team.CreatedAt},
		LoggedIn: authed, User: userViewOf(user),
		CSRFToken: ensureCSRF(w, r), Lang: string(resolveLang(r)),
	}
	s.renderPageForRequest(w, r, "Join team", "", "invite-accept", &data)
}

// ----- POST handlers (settings actions) ----------------------------

func userViewOf(u appmodel.UserIdentity) userView {
	return userView{ID: u.ID, Email: u.Email, Name: u.Name}
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
	case "transferred":
		return i18n.T(lang, "flash.transferred"), true
	case "transfer_personal":
		return i18n.T(lang, "flash.transferPersonal"), false
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
func logoURL(v string) template.URL {
	if strings.HasPrefix(v, "data:image/png;base64,") || strings.HasPrefix(v, "data:image/jpeg;base64,") {
		return template.URL(v)
	}
	return ""
}
