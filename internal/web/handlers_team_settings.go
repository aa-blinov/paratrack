package web

import (
	"encoding/base64"
	"errors"
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) handleAPITeamRename(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/team?flash=bad_request", http.StatusSeeOther)
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	if err := s.services.Teams.Administration.Rename(r.Context(), appmodel.TeamRenameRequest{TeamID: team.ID, CallerID: authenticatedUserID(r), Name: name}); err != nil {
		http.Redirect(w, r, "/settings/team?flash="+encodeFlash(false, s.teamErrorMessage(r, err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/team?flash=renamed", http.StatusSeeOther)
}

// handleAPITeamCalendar sets the workspace's time zone and first day of the week. A
// blank choice gives it back to each member (ADM-07).
func (s *Server) handleAPITeamCalendar(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	request := appmodel.TeamCalendarRequest{
		TeamID: team.ID, CallerID: authenticatedUserID(r),
		TimeZone: strings.TrimSpace(r.FormValue("timezone")), WeekStart: r.FormValue("week_start"),
	}
	if err := s.services.Teams.Administration.SetCalendar(r.Context(), request); err != nil {
		http.Redirect(w, r, "/settings/team?flash="+encodeFlash(false, s.teamErrorMessage(r, err))+"#calendar", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/team?flash=updated#calendar", http.StatusSeeOther)
}

// handleAPITeamCurrency sets the workspace currency. Documents already
// issued keep theirs; new projects without their own currency follow it.

func (s *Server) handleAPITeamCurrency(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	cur := r.FormValue("currency")
	if !validCurrency(cur) {
		http.Redirect(w, r, "/settings/money?flash=bad_request"+"#currency", http.StatusSeeOther)
		return
	}
	if err := s.services.TeamOps.UpdateCurrency(operationContext(r), appmodel.TeamCurrencyRequest{TeamID: team.ID, CallerID: authenticatedUserID(r), Currency: cur}); err != nil {
		http.Redirect(w, r, "/settings/money?flash="+encodeFlash(false, s.teamErrorMessage(r, err))+"#currency", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/money?flash=updated"+"#currency", http.StatusSeeOther)
}

// handleAPITeamRequisites saves the issuer's details and VAT line printed
// on new invoices and acts.

func (s *Server) handleAPITeamRequisites(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	req := strings.TrimSpace(r.FormValue("requisites"))
	vat := strings.TrimSpace(r.FormValue("vat_note"))
	hide := r.FormValue("hide_executor") != ""
	if err := s.services.Teams.Settings.UpdateRequisites(r.Context(), appmodel.TeamRequisitesRequest{TeamID: team.ID, CallerID: authenticatedUserID(r), Requisites: req, VATNote: vat, HideExecutorNames: hide}); errors.Is(err, appmodel.ErrInvalidTeamSettings) {
		http.Redirect(w, r, "/settings/money?flash=bad_request"+"#requisites", http.StatusSeeOther)
		return
	} else if err != nil {
		http.Redirect(w, r, "/settings/money?flash="+encodeFlash(false, s.teamErrorMessage(r, err))+"#requisites", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/money?flash=updated"+"#requisites", http.StatusSeeOther)
}

// handleAPITeamBilling saves the invoice rules: rounding and number prefix.
func (s *Server) handleAPITeamBilling(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	roundMinutes, err := strconv.Atoi(r.FormValue("round_minutes"))
	if err != nil {
		http.Redirect(w, r, "/settings/money?flash=bad_request"+"#billing", http.StatusSeeOther)
		return
	}
	roundMode := r.FormValue("round_mode")
	if roundMode == "" {
		roundMode = "nearest"
	}
	b := model.BillingRules{
		RoundMinutes:  roundMinutes,
		RoundMode:     roundMode,
		InvoicePrefix: strings.TrimSpace(r.FormValue("invoice_prefix")),
	}
	if err := s.services.TeamOps.UpdateBilling(operationContext(r), appmodel.TeamBillingRequest{TeamID: team.ID, CallerID: authenticatedUserID(r), Rules: b}); errors.Is(err, appmodel.ErrInvalidTeamBilling) {
		// Named for what is wrong: the rules, not the form (the flash used to read «Некорректная отправка формы»).
		http.Redirect(w, r, "/settings/money?flash="+encodeFlash(false, i18n.T(resolveLang(r), "flash.msg.invalid billing settings"))+"#billing", http.StatusSeeOther)
		return
	} else if err != nil {
		http.Redirect(w, r, "/settings/money?flash="+encodeFlash(false, s.teamErrorMessage(r, err))+"#billing", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/money?flash=updated"+"#billing", http.StatusSeeOther)
}

// handleAPITeamLogo stores a small PNG/JPEG as a data URL for invoices
// and acts; "remove" clears it.

func (s *Server) handleAPITeamLogo(w http.ResponseWriter, r *http.Request) {
	team, _ := TeamFrom(r.Context())
	const max = 200 << 10
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	if err := r.ParseMultipartForm(2 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		http.Redirect(w, r, "/settings/money?flash=logo_big"+"#logo", http.StatusSeeOther)
		return
	}
	logo := ""
	if r.FormValue("remove") == "" {
		f, _, err := r.FormFile("logo")
		if err != nil {
			http.Redirect(w, r, "/settings/money?flash=bad_request"+"#logo", http.StatusSeeOther)
			return
		}
		defer f.Close()
		raw, err := io.ReadAll(io.LimitReader(f, max+1))
		if err != nil {
			http.Redirect(w, r, "/settings/money?flash=bad_request"+"#logo", http.StatusSeeOther)
			return
		}
		if len(raw) > max {
			http.Redirect(w, r, "/settings/money?flash=logo_big"+"#logo", http.StatusSeeOther)
			return
		}
		ct := http.DetectContentType(raw)
		if ct != "image/png" && ct != "image/jpeg" {
			http.Redirect(w, r, "/settings/money?flash=logo_type"+"#logo", http.StatusSeeOther)
			return
		}
		logo = "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(raw)
	}
	if err := s.services.Teams.Settings.UpdateLogo(r.Context(), appmodel.TeamLogoRequest{TeamID: team.ID, CallerID: authenticatedUserID(r), DataURL: logo}); errors.Is(err, appmodel.ErrInvalidTeamSettings) {
		http.Redirect(w, r, "/settings/money?flash=logo_type"+"#logo", http.StatusSeeOther)
		return
	} else if err != nil {
		http.Redirect(w, r, "/settings/money?flash="+encodeFlash(false, s.teamErrorMessage(r, err))+"#logo", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/money?flash=updated"+"#logo", http.StatusSeeOther)
}

// handleTeamStripe saves Stripe credentials from workspace settings.
func (s *Server) handleTeamStripe(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/money?flash="+encodeFlash(false, i18n.T(resolveLang(r), "flash.msg.invalid form"))+"#stripe", http.StatusSeeOther)
		return
	}
	key := strings.TrimSpace(r.PostForm.Get("stripe_key"))
	secret := strings.TrimSpace(r.PostForm.Get("stripe_webhook_secret"))
	clear := r.PostForm.Get("clear") == "1"
	if key == "" && secret == "" && !clear {
		// Nothing typed is nothing saved; «Сохранено» here used to cover a wipe.
		http.Redirect(w, r, "/settings/money?flash="+encodeFlash(false, i18n.T(resolveLang(r), "stripe.nothing"))+"#stripe", http.StatusSeeOther)
		return
	}
	if err := s.services.TeamOps.SetStripeCredentials(operationContext(r), appmodel.TeamStripeCredentialsRequest{TeamID: teamID(r), CallerID: authenticatedUserID(r), Key: key, Secret: secret, Clear: clear}); errors.Is(err, appmodel.ErrInvalidTeamSettings) {
		// Named for the prefixes, so the person can see what to paste again.
		http.Redirect(w, r, "/settings/money?flash="+encodeFlash(false, i18n.T(resolveLang(r), "stripe.badFormat"))+"#stripe", http.StatusSeeOther)
		return
	} else if err != nil {
		http.Redirect(w, r, "/settings/money?flash="+encodeFlash(false, s.teamErrorMessage(r, err))+"#stripe", http.StatusSeeOther)
		return
	}
	done := "updated"
	if clear {
		done = "stripe_cleared"
	}
	http.Redirect(w, r, "/settings/money?flash="+encodeFlash(true, done)+"#stripe", http.StatusSeeOther)
}

func (s *Server) teamErrorMessage(r *http.Request, err error) string {
	switch {
	case errors.Is(err, appmodel.ErrTeamValidation) && strings.Contains(err.Error(), "already a member"):
		return i18n.T(resolveLang(r), "flash.msg.already a member")
	case strings.Contains(err.Error(), "invite already used"):
		return i18n.T(resolveLang(r), "flash.msg.invite already used")
	case strings.Contains(err.Error(), "invite expired"):
		return i18n.T(resolveLang(r), "flash.msg.invite expired")
	case errors.Is(err, appmodel.ErrTeamNotFound), errors.Is(err, model.ErrNotFound):
		return "workspace not found"
	case errors.Is(err, appmodel.ErrTeamDuplicate), errors.Is(err, model.ErrAlreadyExists):
		return "workspace name is already in use"
	case errors.Is(err, appmodel.ErrTeamValidation):
		return i18n.T(resolveLang(r), "err.invalidInput")
	case errors.Is(err, appmodel.ErrTeamForbidden), errors.Is(err, model.ErrForbidden):
		return "manager role required"
	case errors.Is(err, appmodel.ErrInvalidTeamBilling):
		return "invalid billing settings"
	case errors.Is(err, appmodel.ErrInvalidTeamSettings):
		return "invalid workspace settings"
	case errors.Is(err, model.ErrInviteExpired):
		return "invite expired"
	case errors.Is(err, model.ErrLastOwner):
		return "cannot remove the last workspace owner"
	case errors.Is(err, model.ErrOwnerMustTransfer):
		return "transfer ownership before removing the current owner"
	case errors.Is(err, model.ErrTeamHasMembers):
		return "remove other workspace members first"
	default:
		s.logInternalError(err)
		return i18n.T(resolveLang(r), "err.internal")
	}
}

// logoURL lets a stored logo into an <img src>; only the two data: forms
// the upload handler writes pass.
