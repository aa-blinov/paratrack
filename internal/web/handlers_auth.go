package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// authPage is the shared envelope for login / register / password
// recovery pages. T() exposes the i18n dictionary to templates.
type authPage struct {
	AuthReact string
	Title     string
	ErrorMsg  string
	InfoMsg   string
	Email     string
	Name      string
	Next      string
	Token     string
	CSRFToken string
	Lang      string
	SSO       bool
}

func (authPage) isTemplateData()      {}
func (p authPage) usesReactApp() bool { return p.AuthReact != "" }

func (p authPage) T(key string) string { return i18n.T(i18n.Lang(p.Lang), key) }

func (p *authPage) setCSRF(token string) { p.CSRFToken = token }
func (p *authPage) setLang(lang string)  { p.Lang = lang }

// handleLogin renders the login form.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	data := authPage{
		AuthReact: "login",
		Title:     "Log in",
		Next:      r.URL.Query().Get("next"),
		SSO:       s.config.OIDCEnabled,
	}
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		data.ErrorMsg = humaniseAuthError(errMsg, resolveLang(r))
	}
	data.CSRFToken = ensureCSRF(w, r)
	data.Lang = string(resolveLang(r))
	s.renderPage(w, r, "Log in", "", "login", &data)
}

// handleRegister renders the registration form.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	data := authPage{
		AuthReact: "register",
		Title:     "Sign up",
		Next:      r.URL.Query().Get("next"),
	}
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		data.ErrorMsg = humaniseAuthError(errMsg, resolveLang(r))
	}
	data.CSRFToken = ensureCSRF(w, r)
	data.Lang = string(resolveLang(r))
	s.renderPage(w, r, "Sign up", "", "register", &data)
}

// handleAPILogin accepts the login form submission, verifies the
// credentials and (on success) sets the session cookie + redirects.
func (s *Server) handleAPILogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/login?error=bad_request", http.StatusSeeOther)
		return
	}
	email := strings.TrimSpace(r.PostForm.Get("email"))
	password := r.PostForm.Get("password")
	next := strings.TrimSpace(r.PostForm.Get("next"))
	if email == "" || password == "" {
		http.Redirect(w, r, "/login?error=missing_fields", http.StatusSeeOther)
		return
	}

	_, sess, err := s.services.Auth.SignIn.AuthenticatePassword(operationContext(r), appmodel.PasswordLoginRequest{Email: email, Password: password})
	if errors.Is(err, appmodel.ErrAuthCredentialsInvalid) {
		// Same message for "no such user" and "wrong password" — don't
		// leak which one it was.
		http.Redirect(w, r, "/login?error=bad_credentials", http.StatusSeeOther)
		return
	}
	if err != nil {
		http.Redirect(w, r, "/login?error=internal", http.StatusSeeOther)
		return
	}
	setSessionCookie(w, r, sess.Token)

	redirect := "/"
	if next != "" && strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") {
		redirect = next
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

// handleAPIRegister creates a new user + personal team and logs them
// in. Failures are surfaced via ?error=… redirects back to /register.
func (s *Server) handleAPIRegister(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/register?error=bad_request", http.StatusSeeOther)
		return
	}
	email := strings.TrimSpace(r.PostForm.Get("email"))
	password := r.PostForm.Get("password")
	name := strings.TrimSpace(r.PostForm.Get("name"))
	next := strings.TrimSpace(r.PostForm.Get("next"))
	// An error must not lose where the user was going (an invite link).
	keep := ""
	if strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") {
		keep = "&next=" + url.QueryEscape(next)
	}
	if email == "" || password == "" || name == "" {
		http.Redirect(w, r, "/register?error=missing_fields"+keep, http.StatusSeeOther)
		return
	}

	teamName := localizedPersonalTeamName(resolveLang(r), name)
	sess, _, err := s.services.Auth.SignIn.RegisterAndStartSession(operationContext(r), appmodel.RegistrationRequest{
		Email: email, Password: password, Name: name, TeamName: teamName,
	})
	if err != nil {
		code := "validation_failed"
		switch {
		case errors.Is(err, appmodel.ErrAuthInvalidEmail):
			code = "bad_email"
		case errors.Is(err, appmodel.ErrAuthValidation):
			code = "validation_failed"
		default:
			code = "could_not_register"
		}
		http.Redirect(w, r, "/register?error="+code+keep, http.StatusSeeOther)
		return
	}
	setSessionCookie(w, r, sess.Token)
	// A fresh account answers one question first: what the app is for.
	redirect := "/welcome"
	if next != "" && strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") {
		redirect = next
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func localizedPersonalTeamName(lang i18n.Lang, name string) string {
	return strings.ReplaceAll(i18n.T(lang, "team.personalName"), "{name}", strings.TrimSpace(name))
}

// handleAPILogout kills the current session and bounces to /login.
func (s *Server) handleAPILogout(w http.ResponseWriter, r *http.Request) {
	token := ""
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		token = cookie.Value
	}
	if err := s.services.Auth.Identity.Logout(operationContext(r), appmodel.LogoutRequest{TeamID: teamID(r), UserID: authenticatedUserID(r), Token: token}); err != nil {
		s.logInternalError(fmt.Errorf("delete logout session: %w", err))
	}
	clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// humaniseAuthError maps the ?error=… code on the redirect URL back to
// a human-friendly sentence shown above the form.
func humaniseAuthError(code string, lang i18n.Lang) string {
	switch code {
	case "bad_credentials":
		return i18n.T(lang, "err.badCredentials")
	case "missing_fields":
		return i18n.T(lang, "err.missingFields")
	case "bad_email":
		return i18n.T(lang, "err.badEmail")
	case "validation_failed":
		return i18n.T(lang, "err.validation")
	case "could_not_register":
		return i18n.T(lang, "err.emailTaken")
	case "reset_invalid":
		return i18n.T(lang, "err.resetInvalid")
	case "internal":
		return i18n.T(lang, "err.internal")
	default:
		return i18n.T(lang, "err.generic")
	}
}
