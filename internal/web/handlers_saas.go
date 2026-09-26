package web

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/auth"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

// ---------------------------------------------------------------------------
// Password recovery
// ---------------------------------------------------------------------------

// handleForgotPassword renders the "forgot password" form.
func (s *Server) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	data := authPage{Title: "Reset password"}
	if v := r.URL.Query().Get("error"); v != "" {
		data.ErrorMsg = humaniseAuthError(v, resolveLang(r))
	}
	if v := r.URL.Query().Get("sent"); v == "1" {
		data.InfoMsg = "If that address has an account, a reset link is on its way. Check your inbox (and the server log if mail is not configured)."
	}
	data.Email = r.URL.Query().Get("email")
	data.CSRFToken = ensureCSRF(w, r)
	data.Lang = string(resolveLang(r))
	s.renderPage(w, r, "Reset password", "", "forgot-password", data)
}

// handleResetPassword renders the "set a new password" form for a
// token that arrived by email. The token is validated only on submit —
// the page itself just echoes it back so the link is clickable without
// a pre-check round-trip.
func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	data := authPage{Title: "Choose a new password", Token: r.URL.Query().Get("token")}
	if v := r.URL.Query().Get("error"); v != "" {
		data.ErrorMsg = humaniseAuthError(v, resolveLang(r))
	}
	data.CSRFToken = ensureCSRF(w, r)
	data.Lang = string(resolveLang(r))
	s.renderPage(w, r, "Choose a new password", "", "reset-password", data)
}

// handleAPIPasswordForgot always answers with the same "sent" page so
// the endpoint cannot be used to enumerate addresses. Rate-limited at
// the route layer.
func (s *Server) handleAPIPasswordForgot(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	email := strings.TrimSpace(r.PostForm.Get("email"))
	redirect := "/forgot-password?sent=1"
	if email != "" {
		redirect += "&email=" + url.QueryEscape(email)
	}
	// Best-effort: only mint a token when the address exists. Swallow
	// every error into the same redirect.
	if user, err := s.auth.FindByEmail(r.Context(), email); err == nil {
		if token, err := s.auth.CreatePasswordReset(r.Context(), user.ID); err == nil {
			base := publicBaseURL(r)
			link := base + "/reset-password?token=" + url.QueryEscape(token)
			body := "Hi " + user.Name + ",\n\n" +
				"Someone (hopefully you) asked to reset the password for " + user.Email + ".\n\n" +
				"Open this link within " + auth.ResetTTL.String() + " to choose a new password:\n\n" +
				link + "\n\n" +
				"If you didn't ask for this, you can ignore this message — the link is single-use and expires quickly.\n\n" +
				"— paratrack\n"
			_ = s.mailer.Send(user.Email, "Reset your paratrack password", body)
		}
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

// handleAPIPasswordReset consumes the token and sets the new password.
func (s *Server) handleAPIPasswordReset(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	token := strings.TrimSpace(r.PostForm.Get("token"))
	pw := r.PostForm.Get("new_password")
	if token == "" || pw == "" {
		http.Redirect(w, r, "/reset-password?error=missing_fields&token="+url.QueryEscape(token), http.StatusSeeOther)
		return
	}
	user, err := s.auth.ConsumePasswordReset(r.Context(), token, pw)
	if err != nil {
		code := "reset_invalid"
		if errors.Is(err, auth.ErrValidation) {
			code = "validation_failed"
		}
		http.Redirect(w, r, "/reset-password?error="+code+"&token="+url.QueryEscape(token), http.StatusSeeOther)
		return
	}
	// Log the user in on the new password immediately.
	sess, err := s.auth.NewSession(r.Context(), user.ID)
	if err != nil {
		http.Redirect(w, r, "/login?error=internal", http.StatusSeeOther)
		return
	}
	setSessionCookie(w, r, sess.Token)
	http.Redirect(w, r, "/?flash=password_reset", http.StatusSeeOther)
}

// publicBaseURL reconstructs scheme://host for links we email. Behind
// TLS we honour X-Forwarded-Proto so the link is https://.
func publicBaseURL(r *http.Request) string {
	scheme := "http"
	if isSecureRequest(r) {
		scheme = "https"
	}
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		return scheme + "://" + strings.TrimSpace(h)
	}
	return scheme + "://" + r.Host
}

// ---------------------------------------------------------------------------
// Web backfill — create a closed session without the CLI.
// ---------------------------------------------------------------------------

// handleBackfill creates a finished session from the dashboard form.
// Accepts natural-language times (same parser as the CLI `add`).
func (s *Server) handleBackfill(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	name := strings.TrimSpace(r.FormValue("activity"))
	startStr := strings.TrimSpace(r.FormValue("start"))
	endStr := strings.TrimSpace(r.FormValue("end"))
	note := strings.TrimSpace(r.FormValue("note"))
	if name == "" || startStr == "" || endStr == "" {
		s.toast(w, "activity, start and end are required", "error")
		s.respondActiveList(w, r) // keep the HTMX target happy
		return
	}
	now := time.Now()
	start, err := timeparse.ParseDateTime(startStr, now)
	if err != nil {
		s.toast(w, "bad start: "+err.Error(), "error")
		s.respondActiveList(w, r)
		return
	}
	end, err := timeparse.ParseDateTime(endStr, now)
	if err != nil {
		s.toast(w, "bad end: "+err.Error(), "error")
		s.respondActiveList(w, r)
		return
	}
	if !end.After(start) {
		s.toast(w, "end must be after start", "error")
		s.respondActiveList(w, r)
		return
	}
	act, err := s.db.GetOrCreateActivity(r.Context(), teamID(r), name)
	if err != nil {
		s.toast(w, err.Error(), "error")
		s.respondActiveList(w, r)
		return
	}
	if pidStr := r.FormValue("project_id"); pidStr != "" {
		if pid, err := strconv.ParseInt(pidStr, 10, 64); err == nil && pid > 0 {
			if err := s.db.AssignActivityProject(r.Context(), teamID(r), act.ID, pid); err != nil {
				s.toast(w, "project: "+err.Error(), "error")
				s.respondActiveList(w, r)
				return
			}
		}
	}
	if _, err := s.db.CreateClosedSession(r.Context(), teamID(r), act.ID, start, end, note); err != nil {
		s.toast(w, err.Error(), "error")
		s.respondActiveList(w, r)
		return
	}
	s.toastL(w, r, "toast.added", act.Name+" "+start.Format("15:04")+"→"+end.Format("15:04"), "success")
	// Refresh the active list (unchanged) so HTMX has a target; the
	// user then looks at /stats for the closed row.
	s.respondActiveList(w, r)
}


// handleSetLang switches the UI language and returns to the previous page.
func (s *Server) handleSetLang(w http.ResponseWriter, r *http.Request) {
	code := i18n.Normalize(r.PathValue("code"))
	next := r.URL.Query().Get("next")
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = "/"
	}
	// Already on this language — do not touch the cookie or force a
	// needless round-trip. (The UI also marks the current language as
	// non-clickable; this covers deep links and old bookmarks.)
	if resolveLang(r) == code {
		if c, err := r.Cookie(langCookieName); err == nil && c.Value == string(code) {
			http.Redirect(w, r, next, http.StatusSeeOther)
			return
		}
	}
	setLangCookie(w, r, code)
	http.Redirect(w, r, next, http.StatusSeeOther)
}
