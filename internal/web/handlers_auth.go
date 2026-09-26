package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/aa-blinov/paratrack/internal/auth"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// authPage is the shared envelope for login / register / password
// recovery pages. T() exposes the i18n dictionary to templates.
type authPage struct {
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

func (p authPage) T(key string) string { return i18n.T(i18n.Lang(p.Lang), key) }

// handleLogin renders the login form.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	data := authPage{
		Title: "Log in",
		Next:  r.URL.Query().Get("next"),
		SSO:   ssoConfigured(),
	}
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		data.ErrorMsg = humaniseAuthError(errMsg, resolveLang(r))
	}
	data.CSRFToken = ensureCSRF(w, r)
	data.Lang = string(resolveLang(r))
	s.renderPage(w, r, "Log in", "", "login", data)
}

// handleRegister renders the registration form.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	data := authPage{
		Title: "Sign up",
		Next:  r.URL.Query().Get("next"),
	}
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		data.ErrorMsg = humaniseAuthError(errMsg, resolveLang(r))
	}
	data.CSRFToken = ensureCSRF(w, r)
	data.Lang = string(resolveLang(r))
	s.renderPage(w, r, "Sign up", "", "register", data)
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

	user, err := s.auth.FindByEmail(r.Context(), email)
	if err != nil {
		// Same message for "no such user" and "wrong password" — don't
		// leak which one it was.
		http.Redirect(w, r, "/login?error=bad_credentials", http.StatusSeeOther)
		return
	}
	if err := s.auth.VerifyPassword(user, password); err != nil {
		http.Redirect(w, r, "/login?error=bad_credentials", http.StatusSeeOther)
		return
	}

	sess, err := s.auth.NewSession(r.Context(), user.ID)
	if err != nil {
		http.Redirect(w, r, "/login?error=internal", http.StatusSeeOther)
		return
	}
	setSessionCookie(w, r, sess.Token)
	s.audit(r, "auth.login", user.Email, "")

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
	if email == "" || password == "" || name == "" {
		http.Redirect(w, r, "/register?error=missing_fields", http.StatusSeeOther)
		return
	}

	userID, teamID, err := s.auth.CreateUser(r.Context(), email, password, name)
	if err != nil {
		code := "validation_failed"
		switch {
		case errors.Is(err, auth.ErrInvalidEmail):
			code = "bad_email"
		case errors.Is(err, auth.ErrValidation):
			code = "validation_failed"
		default:
			code = "could_not_register"
		}
		http.Redirect(w, r, "/register?error="+code, http.StatusSeeOther)
		return
	}

	// The personal team is created as "<Name>'s workspace"; name it in
	// the visitor's language ("Пространство: Аня"). Best effort.
	_ = s.teams.Rename(r.Context(), teamID, strings.ReplaceAll(i18n.T(resolveLang(r), "team.personalName"), "{name}", name))

	sess, err := s.auth.NewSession(r.Context(), userID)
	if err != nil {
		http.Redirect(w, r, "/register?error=internal", http.StatusSeeOther)
		return
	}
	setSessionCookie(w, r, sess.Token)
	redirect := "/"
	if next != "" && strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") {
		redirect = next
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

// handleAPILogout kills the current session and bounces to /login.
func (s *Server) handleAPILogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.CookieName); err == nil {
		_ = s.auth.DeleteByToken(r.Context(), cookie.Value)
	}
	clearSessionCookie(w)
	s.audit(r, "auth.logout", "", "")
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