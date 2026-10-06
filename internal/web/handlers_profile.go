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
	Active       string
	User         userView
	CanManage    bool
	Flash        string
	FlashOK      bool
	CSRFToken    string
	Lang         string
	ProfileReact bool
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
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, "Profile", "settings", "profile", &data)
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
