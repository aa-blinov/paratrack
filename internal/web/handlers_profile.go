package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

type profilePageData struct {
	Active    string
	User      userView
	CanManage bool
	// A member sees the workspace they belong to and can leave it; an owner
	// transfers ownership first, so the owner gets no button here.
	Team         teamView
	Role         string
	CanLeave     bool
	Flash        string
	FlashOK      bool
	CSRFToken    string
	Lang         string
	ProfileReact bool
	// The viewer's own pay in this workspace, read-only (FIN-11): the lines, and the
	// sums still to be paid and already paid.
	Pay     []profilePayRow
	PayOpen string
	PayPaid string
}

// profilePayRow is one of the viewer's own pay lines as the profile shows it.
type profilePayRow struct {
	Run    string
	Period string
	Hours  string
	Rate   string
	Amount string
	Paid   bool
}

func (profilePageData) isTemplateData() {}

func (p *profilePageData) setCSRF(token string) { p.CSRFToken = token }
func (p *profilePageData) setLang(lang string)  { p.Lang = lang }
func (p *profilePageData) setManage(canManage bool) {
	p.CanManage = canManage
}

func (p profilePageData) T(key string) string {
	return i18n.T(i18n.Lang(p.Lang), key)
}

func (profilePageData) usesReactApp() bool { return true }

func (s *Server) handleSettingsProfile(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	data := profilePageData{
		Active:       "settings-profile",
		User:         userViewOf(user),
		ProfileReact: true,
	}
	if team, ok := TeamFrom(r.Context()); ok {
		role := RoleFrom(r.Context())
		data.Team = teamView{ID: team.ID, Name: team.Name, CreatedAt: team.CreatedAt}
		data.Role = string(role)
		data.CanLeave = role != model.TeamRoleOwner
		lines, err := s.services.Payroll.MemberLines(r.Context(), appmodel.MemberPayrollQuery{TeamID: team.ID, UserID: user.ID})
		if err != nil {
			s.writeInternalError(w, err)
			return
		}
		data.Pay, data.PayOpen, data.PayPaid = profilePay(r, lines)
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, "Profile", "settings", &data)
}

// handleAPITeamLeave removes the caller's own membership. The service refuses
// the last owner, and a member's personal workspace stays where it is.
// profilePay turns the viewer's pay lines into rows, and sums what is still to be paid
// and what is paid. The sums use the currency of the first line; a line in another
// currency stays in the rows and out of the sums, so no total mixes currencies.
func profilePay(r *http.Request, lines []appmodel.MemberPayrollLine) ([]profilePayRow, string, string) {
	lang := resolveLang(r)
	rows := make([]profilePayRow, 0, len(lines))
	if len(lines) == 0 {
		return rows, "", ""
	}
	currency := lines[0].Currency
	open, paid := 0, 0
	for _, line := range lines {
		isPaid := line.RunStatus == "paid"
		rows = append(rows, profilePayRow{
			Run:    line.RunNumber,
			Period: periodSpan(lang, fmtDay(lang, line.PeriodStart), fmtDay(lang, line.PeriodEnd.AddDate(0, 0, -1))),
			Hours:  fmtDur(r, line.Seconds),
			Rate:   moneyL(lang, line.RateCents, line.Currency),
			Amount: moneyL(lang, line.AmountCents, line.Currency),
			Paid:   isPaid,
		})
		if line.Currency != currency {
			continue
		}
		if isPaid {
			paid += line.AmountCents
		} else {
			open += line.AmountCents
		}
	}
	return rows, moneyL(lang, open, currency), moneyL(lang, paid, currency)
}

func (s *Server) handleAPITeamLeave(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	user, _ := UserFrom(r.Context())
	if err := s.services.Teams.Administration.RemoveMember(r.Context(), appmodel.TeamMemberRemovalRequest{TeamID: team.ID, TargetUserID: user.ID, CallerID: user.ID}); err != nil {
		http.Redirect(w, r, "/settings/profile?flash="+encodeFlash(false, s.teamErrorMessage(r, err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/profile?flash=left", http.StatusSeeOther)
}

func (s *Server) handleAPIProfileUpdate(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/profile?flash=bad_request", http.StatusSeeOther)
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	if err := s.services.Auth.Profile.UpdateName(r.Context(), appmodel.ProfileNameRequest{UserID: user.ID, CallerID: user.ID, Name: name}); err != nil {
		http.Redirect(w, r, "/settings/profile?flash="+encodeFlash(false, s.profileErrorMessage(r, err)), http.StatusSeeOther)
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
	newPassword := r.PostForm.Get("new_password")
	currentPassword := r.PostForm.Get("current_password")
	// A live session alone must not be enough to take over the account.
	if err := s.services.Auth.Profile.ChangePassword(r.Context(), appmodel.PasswordChangeRequest{UserID: user.ID, CallerID: user.ID, CurrentPassword: currentPassword, NewPassword: newPassword}); errors.Is(err, appmodel.ErrAuthBadPassword) {
		http.Redirect(w, r, "/settings/profile?flash="+encodeFlash(false, "current password is wrong"), http.StatusSeeOther)
		return
	} else if err != nil {
		http.Redirect(w, r, "/settings/profile?flash="+encodeFlash(false, s.profileErrorMessage(r, err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/profile?flash=password_updated", http.StatusSeeOther)
}

// handleAPIProfileEmail moves the login address. The form posts
// current_password together with the new email: the address decides where
// the password reset link goes, so it needs the same proof of ownership as
// changing the password itself.
func (s *Server) handleAPIProfileEmail(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/profile?flash=bad_request", http.StatusSeeOther)
		return
	}
	err := s.services.Auth.Profile.ChangeEmail(r.Context(), appmodel.ProfileEmailRequest{
		UserID: user.ID, CallerID: user.ID,
		CurrentPassword: r.PostForm.Get("current_password"),
		Email:           r.PostForm.Get("email"),
	})
	switch {
	case errors.Is(err, appmodel.ErrAuthBadPassword):
		http.Redirect(w, r, "/settings/profile?flash="+encodeFlash(false, "profile.current password is wrong"), http.StatusSeeOther)
	case errors.Is(err, appmodel.ErrAuthEmailTaken):
		http.Redirect(w, r, "/settings/profile?flash="+encodeFlash(false, "profile.email is taken"), http.StatusSeeOther)
	case errors.Is(err, appmodel.ErrAuthInvalidEmail):
		http.Redirect(w, r, "/settings/profile?flash="+encodeFlash(false, "profile.email is invalid"), http.StatusSeeOther)
	case err != nil:
		http.Redirect(w, r, "/settings/profile?flash="+encodeFlash(false, s.profileErrorMessage(r, err)), http.StatusSeeOther)
	default:
		http.Redirect(w, r, "/settings/profile?flash=email_updated", http.StatusSeeOther)
	}
}

func (s *Server) profileErrorMessage(r *http.Request, err error) string {
	switch {
	case errors.Is(err, appmodel.ErrAuthValidation):
		return i18n.T(resolveLang(r), "err.invalidInput")
	case errors.Is(err, appmodel.ErrAuthNotFound), errors.Is(err, model.ErrNotFound):
		return "profile not found"
	default:
		s.logInternalError(err)
		return i18n.T(resolveLang(r), "err.internal")
	}
}
