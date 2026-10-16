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
	// InviteTeam names the workspace when the person came from an invite
	// link: «С возвращением» alone did not say why they were asked to sign in.
	InviteTeam string
}

func (authPage) isTemplateData()      {}
func (p authPage) usesReactApp() bool { return p.AuthReact != "" }

func (p authPage) T(key string) string { return i18n.T(i18n.Lang(p.Lang), key) }

func (p *authPage) setCSRF(token string) { p.CSRFToken = token }
func (p *authPage) setLang(lang string)  { p.Lang = lang }

// inviteLinkProblem says so when the person came from an invitation link that
// no longer works. Without it the sign-in page looked like a plain sign-in and
// the person could not tell why the invitation did nothing.
func inviteLinkProblem(current, next, team string, lang i18n.Lang) string {
	if current != "" || team != "" || !strings.HasPrefix(next, "/invites/") {
		return current
	}
	return i18n.T(lang, "invite.invalid")
}

// inviteTeamFor is the workspace an invite link in ?next= leads to, or "".
func (s *Server) inviteTeamFor(r *http.Request, next string) string {
	token, ok := strings.CutPrefix(next, "/invites/")
	if !ok || token == "" || strings.ContainsAny(token, "/?#") {
		return ""
	}
	snapshot, err := s.services.Teams.Invitations.InvitePage(r.Context(), token)
	if err != nil {
		return ""
	}
	return snapshot.Team.Name
}

// handleLogin renders the login form.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	data := authPage{
		AuthReact: "login",
		Title:     "Log in",
		Next:      r.URL.Query().Get("next"),
		SSO:       s.config.OIDCEnabled,
	}
	data.InviteTeam = s.inviteTeamFor(r, data.Next)
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		data.ErrorMsg = humaniseAuthError(errMsg, resolveLang(r))
	}
	data.ErrorMsg = inviteLinkProblem(data.ErrorMsg, data.Next, data.InviteTeam, resolveLang(r))
	data.CSRFToken = ensureCSRF(w, r)
	data.Lang = string(resolveLang(r))
	s.renderPage(w, r, "Log in", "", &data)
}

// handleRegister renders the registration form.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	data := authPage{
		AuthReact: "register",
		Title:     "Sign up",
		Next:      r.URL.Query().Get("next"),
	}
	data.InviteTeam = s.inviteTeamFor(r, data.Next)
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		data.ErrorMsg = humaniseAuthError(errMsg, resolveLang(r))
	}
	data.ErrorMsg = inviteLinkProblem(data.ErrorMsg, data.Next, data.InviteTeam, resolveLang(r))
	data.CSRFToken = ensureCSRF(w, r)
	data.Lang = string(resolveLang(r))
	s.renderPage(w, r, "Sign up", "", &data)
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

	user, sess, err := s.services.Auth.SignIn.AuthenticatePassword(operationContext(r), appmodel.PasswordLoginRequest{Email: email, Password: password})
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
	created := s.restoreLastWorkspace(w, r, user)

	redirect := "/"
	if localPath(next) {
		redirect = next
	} else if created {
		// A workspace that was just recreated answers the same question a new
		// account does: which sections are wanted.
		redirect = "/welcome"
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

// restoreLastWorkspace re-hydrates the workspace cookie from the account
// right after a login, so the redirect lands in the space the person was
// working in — including on a device that never held the cookie.
// A remembered space we cannot honour is ignored here; resolveTeam
// re-checks membership on every request and falls back to the personal
// workspace on its own.
func (s *Server) restoreLastWorkspace(w http.ResponseWriter, r *http.Request, user appmodel.UserIdentity) bool {
	if user.ID <= 0 {
		return false
	}
	prefs, err := s.services.Preferences.Load(r.Context(), user.ID)
	if err != nil {
		s.logInternalError(fmt.Errorf("load preferences to restore workspace: %w", err))
		return false
	}
	if prefs.LastTeamID > 0 {
		setTeamCookie(w, r, prefs.LastTeamID)
		return false
	}
	// An account with no workspace at all cannot enter the app: every page
	// resolves one, and /welcome — the page whose whole job is to create the
	// first one — sits behind that same check. Deleting your last workspace
	// therefore locked you out for good, with no way back but a database edit.
	//
	// The product already promises every account a personal workspace, so give
	// it one here and send the person to the section picker.
	memberships, err := s.services.Teams.Directory.MembershipsForUser(r.Context(), user.ID)
	if err != nil {
		s.logInternalError(fmt.Errorf("check memberships to restore workspace: %w", err))
		return false
	}
	if len(memberships) > 0 {
		// resolveTeam falls back to the first membership on its own.
		return false
	}
	team, err := s.services.Teams.Administration.Create(r.Context(), appmodel.TeamCreateRequest{
		OwnerID: user.ID,
		Name:    personalTeamName(user.Name),
	})
	if err != nil {
		s.logInternalError(fmt.Errorf("recreate personal workspace: %w", err))
		return false
	}
	setTeamCookie(w, r, team.ID)
	s.rememberWorkspace(r, user.ID, team.ID)
	return true
}

// rememberWorkspace records the workspace someone moved into, so the next
// login opens it. It is a convenience only: a stale or forged value cannot
// grant access, because resolveTeam re-checks membership per request.
//
// Preferences come from the request context, and a context without them
// means the load failed — better to skip the write than to overwrite a
// preference blob we could not read.
func (s *Server) rememberWorkspace(r *http.Request, userID, teamID int64) {
	if userID <= 0 || teamID <= 0 {
		return
	}
	prefs, ok := r.Context().Value(prefsKey{}).(Prefs)
	if !ok {
		return
	}
	if prefs.LastTeamID == teamID {
		return
	}
	prefs.LastTeamID = teamID
	if err := s.services.Preferences.Save(r.Context(), appmodel.PreferencesSaveRequest{
		UserID: userID, CallerID: userID, TeamID: teamID, Preferences: prefs,
	}); err != nil {
		s.logInternalError(fmt.Errorf("save last workspace: %w", err))
	}
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
	if localPath(next) {
		keep = "&next=" + url.QueryEscape(next)
	}
	if email == "" || password == "" || name == "" {
		http.Redirect(w, r, "/register?error=missing_fields"+keep, http.StatusSeeOther)
		return
	}

	teamName := personalTeamName(name)
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
	if localPath(next) {
		redirect = next
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

// personalTeamName names a personal workspace after its owner. The workspace
// is the owner's own space, so the name alone says it; a prefix only repeats
// the chip that already shows it.
func personalTeamName(name string) string {
	return strings.TrimSpace(name)
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
	// The workspace cookie is this session's scope pointer. It is also
	// what the next login re-hydrates from the account, so dropping it
	// here leaves no previous person's workspace behind on a shared
	// browser while still restoring the right one on the way back in.
	clearTeamCookie(w)
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
