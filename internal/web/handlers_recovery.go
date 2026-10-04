package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

func (s *Server) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	data := authPage{AuthReact: "forgot-password", Title: "Reset password"}
	if v := r.URL.Query().Get("error"); v != "" {
		data.ErrorMsg = humaniseAuthError(v, resolveLang(r))
	}
	if v := r.URL.Query().Get("sent"); v == "1" {
		data.InfoMsg = "If that address has an account, a reset link is on its way. Check your inbox (and the server log if mail is not configured)."
	}
	data.Email = r.URL.Query().Get("email")
	data.CSRFToken = ensureCSRF(w, r)
	data.Lang = string(resolveLang(r))
	s.renderPage(w, r, "Reset password", "", "forgot-password", &data)
}

// handleResetPassword renders the "set a new password" form for a
// token that arrived by email. The token is validated only on submit —
// the page itself just echoes it back so the link is clickable without
// a pre-check round-trip.

func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	data := authPage{AuthReact: "reset-password", Title: "Choose a new password", Token: r.URL.Query().Get("token")}
	if v := r.URL.Query().Get("error"); v != "" {
		data.ErrorMsg = humaniseAuthError(v, resolveLang(r))
	}
	data.CSRFToken = ensureCSRF(w, r)
	data.Lang = string(resolveLang(r))
	s.renderPage(w, r, "Choose a new password", "", "reset-password", &data)
}

// handleAPIPasswordForgot always answers with the same "sent" page so
// the endpoint cannot be used to enumerate addresses. Rate-limited at
// the route layer.

func (s *Server) handleAPIPasswordForgot(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/forgot-password?sent=1", http.StatusSeeOther)
		return
	}
	email := strings.TrimSpace(r.PostForm.Get("email"))
	redirect := "/forgot-password?sent=1"
	if email != "" {
		redirect += "&email=" + url.QueryEscape(email)
	}
	// Best-effort: the auth workflow only mints a token when the address
	// exists. Swallow every error into the same redirect.
	user, token, err := s.services.Auth.Recovery.RequestPasswordReset(r.Context(), appmodel.PasswordResetRequest{Email: email})
	if err == nil {
		base := s.publicBaseURL(r)
		link := base + "/reset-password?token=" + url.QueryEscape(token)
		subj, ev := resetEmail(resolveLang(r), user.Name, user.Email, link, humanTTL(resolveLang(r), appmodel.ResetTTL))
		msg, err := s.buildEmail(user.Email, subj, ev)
		if err != nil {
			s.logger.Printf("password recovery: build reset email: %v", err)
		} else if err := s.deliverPostcommit(r.Context(), msg); err != nil {
			// SMTP replies can echo the recipient; report failure class only.
			s.logger.Printf("password recovery: deliver reset email failed (%T)", err)
		}
	} else if !errors.Is(err, appmodel.ErrAuthNotFound) {
		s.logger.Printf("password recovery: request reset: %v", err)
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

// handleAPIPasswordReset consumes the token and sets the new password.

func (s *Server) handleAPIPasswordReset(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/reset-password?error=missing_fields", http.StatusSeeOther)
		return
	}
	token := strings.TrimSpace(r.PostForm.Get("token"))
	pw := r.PostForm.Get("new_password")
	if token == "" || pw == "" {
		http.Redirect(w, r, "/reset-password?error=missing_fields&token="+url.QueryEscape(token), http.StatusSeeOther)
		return
	}
	_, sess, err := s.services.Auth.Recovery.CompletePasswordReset(r.Context(), appmodel.PasswordResetCompletionRequest{Token: token, NewPassword: pw})
	if err != nil {
		code := "internal"
		if errors.Is(err, appmodel.ErrAuthValidation) {
			code = "validation_failed"
		} else if errors.Is(err, appmodel.ErrAuthResetInvalid) {
			code = "reset_invalid"
		}
		if code == "internal" {
			http.Redirect(w, r, "/login?error=internal", http.StatusSeeOther)
		} else {
			http.Redirect(w, r, "/reset-password?error="+code+"&token="+url.QueryEscape(token), http.StatusSeeOther)
		}
		return
	}
	setSessionCookie(w, r, sess.Token)
	http.Redirect(w, r, "/?flash=password_reset", http.StatusSeeOther)
}
