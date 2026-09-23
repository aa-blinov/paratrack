package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/aa-blinov/paratrack/internal/auth"
)

// handleLogin renders the login form.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Title     string
		Next      string
		ErrorMsg  string
		Email     string
	}{
		Title: "Log in",
		Next:  r.URL.Query().Get("next"),
	}
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		data.ErrorMsg = humaniseAuthError(errMsg)
	}
	s.renderPage(w, "Log in", "", "login", data)
}

// handleRegister renders the registration form.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Title    string
		ErrorMsg string
		Email    string
		Name     string
	}{
		Title: "Sign up",
	}
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		data.ErrorMsg = humaniseAuthError(errMsg)
	}
	s.renderPage(w, "Sign up", "", "register", data)
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
	setSessionCookie(w, sess.Token)

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
	if email == "" || password == "" || name == "" {
		http.Redirect(w, r, "/register?error=missing_fields", http.StatusSeeOther)
		return
	}

	userID, _, err := s.auth.CreateUser(r.Context(), email, password, name)
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

	sess, err := s.auth.NewSession(r.Context(), userID)
	if err != nil {
		http.Redirect(w, r, "/register?error=internal", http.StatusSeeOther)
		return
	}
	setSessionCookie(w, sess.Token)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleAPILogout kills the current session and bounces to /login.
func (s *Server) handleAPILogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.CookieName); err == nil {
		_ = s.auth.DeleteByToken(r.Context(), cookie.Value)
	}
	clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// humaniseAuthError maps the ?error=… code on the redirect URL back to
// a human-friendly sentence shown above the form.
func humaniseAuthError(code string) string {
	switch code {
	case "bad_credentials":
		return "Wrong email or password."
	case "missing_fields":
		return "Email and password are required."
	case "bad_email":
		return "That doesn't look like a valid email address."
	case "validation_failed":
		return "Password must be 8-72 characters. Name is required."
	case "could_not_register":
		return "That email is already taken."
	case "internal":
		return "Something went wrong on our end. Please try again."
	default:
		return "Something went wrong. Please try again."
	}
}