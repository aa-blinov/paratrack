package web

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/aa-blinov/paratrack/internal/auth"
	"github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/teams"
)

// ctxKey is an unexported type so values stored under it can never
// collide with anything else living in r.Context().
type ctxKey int

const (
	ctxUserKey ctxKey = iota
	ctxTeamKey
)

// UserFrom returns the authenticated user attached to r's context, if
// any. The second return is false for anonymous requests.
func UserFrom(ctx context.Context) (auth.User, bool) {
	u, ok := ctx.Value(ctxUserKey).(auth.User)
	return u, ok
}

// TeamFrom returns the current team attached to r's context. It is
// only set when UserFrom is also set.
func TeamFrom(ctx context.Context) (teams.Team, bool) {
	t, ok := ctx.Value(ctxTeamKey).(teams.Team)
	return t, ok
}

// WithUser / WithTeam return a derived context with the given
// principal attached. Mostly useful in tests; production code lets the
// middleware set them.
func WithUser(ctx context.Context, u auth.User) context.Context {
	return context.WithValue(ctx, ctxUserKey, u)
}
func WithTeam(ctx context.Context, t teams.Team) context.Context {
	return context.WithValue(ctx, ctxTeamKey, t)
}

// requireAuth is the actual middleware. Pulled out as a value so it
// can be composed with route-specific work (e.g. CSRF for POSTs).
//
// Behaviour:
//   - If no session cookie or session is invalid → call onFailure (which
//     for pages redirects to /login, for /api/* returns 401 JSON).
//   - Otherwise attach User + Team to the context and call the next
//     handler.
//   - Touches the session so last_seen_at updates on every request.
//   - Also enforces the "current team" cookie: if it points to a team
//     the user is no longer in, fall back to their personal team.
func (s *Server) requireAuth(onFailure func(w http.ResponseWriter, r *http.Request)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Session cookie first; Bearer API token second (extension / CLI).
			token := ""
			if cookie, err := r.Cookie(auth.CookieName); err == nil {
				token = strings.TrimSpace(cookie.Value)
			}
			if token == "" {
				if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
					token = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
				}
			}
			if token == "" {
				onFailure(w, r)
				return
			}
			ctx := r.Context()
			var sess auth.Session
			var user auth.User
			var err error
			var tokTeam int64
			if strings.HasPrefix(token, "pt_") {
				tok, terr := s.db.APITokenByRaw(ctx, token)
				if terr != nil {
					onFailure(w, r)
					return
				}
				if tok.ReadOnly && r.Method != http.MethodGet && r.Method != http.MethodHead {
					writeJSONStatus(w, http.StatusForbidden, map[string]string{"error": "this API token is read-only"})
					return
				}
				tokTeam = tok.TeamID
				user, err = s.auth.FindByID(ctx, tok.UserID)
				if err != nil {
					onFailure(w, r)
					return
				}
				sess = auth.Session{UserID: user.ID, Token: token}
			} else {
				sess, user, err = s.auth.FindByToken(ctx, token)
			}
			if err != nil {
				if errors.Is(err, auth.ErrSessionInvalid) {
					// Clear the dead cookie so the browser stops sending it.
					clearSessionCookie(w)
				}
				onFailure(w, r)
				return
			}
			// Cheap UPDATE; safe on every request.
			s.auth.Touch(ctx, sess.Token)

			team, err := s.resolveTeam(ctx, user, r)
			if tokTeam > 0 {
				// A token acts in the workspace it was made in, while the
				// owner is still a member there.
				if _, ok, merr := s.teams.IsMember(ctx, tokTeam, user.ID); merr == nil && ok {
					team, err = s.teams.FindByID(ctx, tokTeam)
				} else {
					onFailure(w, r)
					return
				}
			}
			if err != nil {
				// User with no membership at all (shouldn't happen with
				// the auto-personal-team flow, but defend against it).
				onFailure(w, r)
				return
			}
			ctx = WithUser(ctx, user)
			ctx = WithTeam(ctx, team)
			role, _, _ := s.teams.IsMember(ctx, team.ID, user.ID)
			ctx = context.WithValue(ctx, ctxRoleKey, role)
			// Every session this request creates is the user's; a member
			// only ever sees their own time.
			ctx = db.WithActor(ctx, user.ID)
			if raw, err := s.db.UserPrefs(ctx, user.ID); err == nil {
				ctx = withPrefs(ctx, parsePrefs(raw))
			}
			if !role.CanManage() {
				ctx = db.WithScope(ctx, user.ID)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// resolveTeam picks which team the current request should be scoped
// to. Priority:
//  1. The paratrack_team cookie, if it points to a team the user is in.
//  2. The user's personal team (the first team they joined).
//
// Falls back to the personal team if the cookie references a team the
// user has been removed from, so a stale cookie can't escape auth.
func (s *Server) resolveTeam(ctx context.Context, user auth.User, r *http.Request) (teams.Team, error) {
	if cookie, err := r.Cookie(teamCookieName); err == nil {
		var wantID int64
		if _, scanErr := scanInt(cookie.Value, &wantID); scanErr == nil && wantID > 0 {
			if role, ok, err := s.teams.IsMember(ctx, wantID, user.ID); err == nil && ok {
				_ = role
				return s.teams.FindByID(ctx, wantID)
			}
		}
	}
	userTeams, err := s.teams.ListForUser(ctx, user.ID)
	if err != nil {
		return teams.Team{}, err
	}
	if len(userTeams) == 0 {
		return teams.Team{}, errors.New("user has no team")
	}
	// ListForUser sorts owners first; the first one is always the
	// user's personal workspace.
	return userTeams[0], nil
}

// pageRedirect sends the browser to /login and adds ?next= so the
// post-login redirect can return them to where they came from.
func pageRedirect(w http.ResponseWriter, r *http.Request) {
	target := "/login"
	if r.URL.Path != "/" || r.URL.RawQuery != "" {
		target += "?next=" + urlEscape(r.URL.RequestURI())
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// apiUnauthorized responds 401 with a tiny JSON body for /api calls.
func apiUnauthorized(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
}

// setSessionCookie writes the HttpOnly session token and ensures the
// browser sends it back on every request to the same origin. Secure is
// set when the request arrived over TLS (directly or via a
// TLS-terminating proxy that stamps X-Forwarded-Proto).
func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(auth.SessionTTL.Seconds()),
		Secure:   isSecureRequest(r),
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// setTeamCookie remembers the user's chosen team across requests.
// SameSite=Lax so it still flows on top-level navigations.
func setTeamCookie(w http.ResponseWriter, r *http.Request, teamID int64) {
	http.SetCookie(w, &http.Cookie{
		Name:     teamCookieName,
		Value:    intToString(teamID),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(auth.SessionTTL.Seconds()),
		Secure:   isSecureRequest(r),
	})
}

const teamCookieName = "paratrack_team"
type roleKey struct{}

var ctxRoleKey = roleKey{}

// RoleFrom is the user's role in the current workspace.
func RoleFrom(ctx context.Context) teams.Role {
	r, _ := ctx.Value(ctxRoleKey).(teams.Role)
	return r
}

func canManage(r *http.Request) bool { return RoleFrom(r.Context()).CanManage() }

// manage guards money and workspace settings: owner and admin only. A
// member gets a plain 403 (JSON on /api/), never a half-rendered page.
func (s *Server) manage(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if canManage(r) {
			h(w, r)
			return
		}
		msg := i18n.T(resolveLang(r), "err.managersOnly")
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get("HX-Request") == "" {
			writeJSONStatus(w, http.StatusForbidden, map[string]string{"error": msg})
			return
		}
		s.toast(w, msg, "error")
		http.Error(w, msg, http.StatusForbidden)
	}
}

// mine scopes a handler to the user's own sessions even for an owner or
// admin: timers and the timesheet are personal (a manager must not stop
// a colleague's timer from their own dashboard).
func (s *Server) mine(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if u, ok := UserFrom(r.Context()); ok {
			r = r.WithContext(db.WithScope(r.Context(), u.ID))
		}
		h(w, r)
	}
}
