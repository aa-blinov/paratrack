package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// twoFactorCookieName carries the open code step between the password and the code. It is
// short-lived and HttpOnly; the session cookie is set only after the code is right.
const twoFactorCookieName = "paratrack_2fa"

func setTwoFactorCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     twoFactorCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: sameSiteFor(r),
		MaxAge:   int(appmodel.ChallengeTTL.Seconds()),
		Secure:   isSecureRequest(r),
	})
}

func clearTwoFactorCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     twoFactorCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: sameSiteFor(r),
		MaxAge:   -1,
		Secure:   isSecureRequest(r),
	})
}

// sendVerificationMail mails a confirmation link. A failed delivery is logged, not shown: the
// person can ask for the link again by signing in.
func (s *Server) sendVerificationMail(r *http.Request, identity appmodel.UserIdentity, token string) {
	link := s.publicBaseURL(r) + "/verify-email?token=" + url.QueryEscape(token)
	subj, vm := verifyEmail(resolveLang(r), identity.Name, identity.Email, link, humanTTL(resolveLang(r), appmodel.VerificationTTL))
	msg, err := s.buildEmail(r, identity.Email, subj, vm)
	if err != nil {
		s.logger.Printf("signup confirmation: build email: %v", err)
		return
	}
	if err := s.deliverPostcommit(r.Context(), msg); err != nil {
		s.logger.Printf("signup confirmation: deliver email failed (%T)", err)
	}
}

// handleVerifyEmail spends a confirmation link and sends the person to sign in.
func (s *Server) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		http.Redirect(w, r, "/login?error=verify_invalid", http.StatusSeeOther)
		return
	}
	if _, err := s.services.Auth.SignIn.ConfirmEmail(operationContext(r), token); err != nil {
		if errors.Is(err, appmodel.ErrAuthVerificationInvalid) {
			http.Redirect(w, r, "/login?error=verify_invalid", http.StatusSeeOther)
			return
		}
		s.logInternalError(err)
		http.Redirect(w, r, "/login?error=internal", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login?notice=verified", http.StatusSeeOther)
}

// handleLogin2FA shows the code step. Without an open step there is nothing to answer, so the
// person goes back to the password.
func (s *Server) handleLogin2FA(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(twoFactorCookieName)
	if err != nil || cookie.Value == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	data := authPage{
		AuthReact: "two-factor",
		Title:     "Two-factor sign-in",
		Next:      r.URL.Query().Get("next"),
	}
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		data.ErrorMsg = humaniseAuthError(errMsg, resolveLang(r))
	}
	data.CSRFToken = ensureCSRF(w, r)
	data.Lang = string(resolveLang(r))
	s.renderPage(w, r, "Two-factor sign-in", "", &data)
}

// handleAPILogin2FA checks the one-time code of the open step and starts the session.
func (s *Server) handleAPILogin2FA(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/login?error=bad_request", http.StatusSeeOther)
		return
	}
	next := strings.TrimSpace(r.PostForm.Get("next"))
	keep := ""
	if localPath(next) {
		keep = "&next=" + url.QueryEscape(next)
	}
	cookie, err := r.Cookie(twoFactorCookieName)
	if err != nil || cookie.Value == "" {
		http.Redirect(w, r, "/login?error=challenge_invalid", http.StatusSeeOther)
		return
	}
	code := strings.TrimSpace(r.PostForm.Get("code"))
	user, sess, err := s.services.Auth.SignIn.CompleteTwoFactor(operationContext(r), appmodel.TwoFactorLoginRequest{Challenge: cookie.Value, Code: code})
	if errors.Is(err, appmodel.ErrAuthTwoFactorInvalid) {
		http.Redirect(w, r, "/login/2fa?error=bad_code"+keep, http.StatusSeeOther)
		return
	}
	if errors.Is(err, appmodel.ErrAuthChallengeInvalid) {
		clearTwoFactorCookie(w, r)
		http.Redirect(w, r, "/login?error=challenge_invalid", http.StatusSeeOther)
		return
	}
	if err != nil {
		clearTwoFactorCookie(w, r)
		s.logInternalError(err)
		http.Redirect(w, r, "/login?error=internal", http.StatusSeeOther)
		return
	}
	clearTwoFactorCookie(w, r)
	setSessionCookie(w, r, sess.Token)
	created := s.restoreLastWorkspace(w, r, user)
	redirect := "/"
	if localPath(next) {
		redirect = next
	} else if created {
		redirect = "/welcome"
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

// twoFactorSetupView is what the profile page gets when a set-up starts: the key to type and the
// link for a phone.
type twoFactorSetupView struct {
	Secret string `json:"secret"`
	URI    string `json:"uri"`
}

func (twoFactorSetupView) isJSONResponse() {}

// recoveryCodesView carries the recovery codes of a freshly enabled second factor. They are
// shown once.
type recoveryCodesView struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

func (recoveryCodesView) isJSONResponse() {}

// writeTwoFactorError answers a refused set-up step with the input it names, or a conflict
// when the step does not fit the account.
func (s *Server) writeTwoFactorError(w http.ResponseWriter, r *http.Request, err error) {
	lang := resolveLang(r)
	switch {
	case errors.Is(err, appmodel.ErrAuthTwoFactorInvalid):
		s.writeJSONStatus(w, http.StatusBadRequest, apiFieldErrorResponse{Error: i18n.T(lang, "auth.err.badCode"), Field: "code"})
	case errors.Is(err, appmodel.ErrAuthBadPassword):
		s.writeJSONStatus(w, http.StatusBadRequest, apiFieldErrorResponse{Error: i18n.T(lang, "err.invalidInput"), Field: "password"})
	case errors.Is(err, appmodel.ErrAuthTwoFactorState):
		s.writeJSONStatus(w, http.StatusConflict, apiErrorResponse{Error: i18n.T(lang, "err.invalidInput")})
	default:
		s.writeInternalJSONError(w, err)
	}
}

// handleAPITwoFactorSetup starts the set-up and returns the key for the authenticator app.
func (s *Server) handleAPITwoFactorSetup(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	setup, err := s.services.Auth.Profile.BeginTwoFactor(r.Context(), user.ID)
	if err != nil {
		s.writeTwoFactorError(w, r, err)
		return
	}
	s.writeJSON(w, twoFactorSetupView{Secret: setup.Secret, URI: setup.URI})
}

// handleAPITwoFactorEnable confirms the set-up with a code from the app and returns the recovery codes.
func (s *Server) handleAPITwoFactorEnable(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	var body struct {
		Code string `json:"code"`
	}
	if !s.decodeJSONBody(w, r, &body) {
		return
	}
	codes, err := s.services.Auth.Profile.ConfirmTwoFactor(r.Context(), appmodel.TwoFactorConfirmRequest{UserID: user.ID, Code: strings.TrimSpace(body.Code)})
	if err != nil {
		s.writeTwoFactorError(w, r, err)
		return
	}
	s.writeJSON(w, recoveryCodesView{RecoveryCodes: codes})
}

// handleAPITwoFactorDisable turns the second factor off with the password and a current code.
func (s *Server) handleAPITwoFactorDisable(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	var body struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !s.decodeJSONBody(w, r, &body) {
		return
	}
	if err := s.services.Auth.Profile.DisableTwoFactor(r.Context(), appmodel.TwoFactorDisableRequest{
		UserID: user.ID, Password: body.Password, Code: strings.TrimSpace(body.Code),
	}); err != nil {
		s.writeTwoFactorError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
