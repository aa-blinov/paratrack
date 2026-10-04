package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// ctxKey is an unexported type so values stored under it can never
// collide with anything else living in r.Context().
type ctxKey int

const (
	ctxUserKey ctxKey = iota
	ctxTeamKey
	ctxTeamModulesKey
)

var errNoTeamMembership = errors.New("user has no team membership")
var errInvalidCredential = errors.New("invalid authentication credential")
var errReadOnlyCredential = errors.New("read-only API token cannot mutate state")

// UserFrom returns the authenticated user attached to r's context, if
// any. The second return is false for anonymous requests.
func UserFrom(ctx context.Context) (appmodel.UserIdentity, bool) {
	u, ok := ctx.Value(ctxUserKey).(appmodel.UserIdentity)
	return u, ok
}

func authenticatedUserID(r *http.Request) int64 {
	user, _ := UserFrom(r.Context())
	return user.ID
}

// TeamFrom returns the current team attached to r's context. It is
// only set when UserFrom is also set.
func TeamFrom(ctx context.Context) (model.Team, bool) {
	t, ok := ctx.Value(ctxTeamKey).(model.Team)
	return t, ok
}

// WithUser / WithTeam return a derived context with the given
// principal attached. Mostly useful in tests; production code lets the
// middleware set them.
func WithUser(ctx context.Context, u appmodel.UserIdentity) context.Context {
	return context.WithValue(ctx, ctxUserKey, u)
}
func WithTeam(ctx context.Context, t model.Team) context.Context {
	return context.WithValue(ctx, ctxTeamKey, t)
}

type authenticatedPrincipal struct {
	sessionToken string
	user         appmodel.UserIdentity
	teamID       int64
	clearSession bool
}

// authenticateRequest resolves either a browser session or API token into a
// principal. Workspace selection and request-context policy happen later, so
// credential validation has one explicit failure boundary.
func (s *Server) authenticateRequest(ctx context.Context, r *http.Request) (authenticatedPrincipal, error) {
	principal := authenticatedPrincipal{}
	credential := ""
	if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
		// Match csrfExempt: when a request declares Bearer authentication,
		// never silently fall back to a browser cookie, even for an empty or
		// invalid token. Otherwise CSRF could be bypassed under cookie auth.
		credential = strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	} else if cookie, err := r.Cookie(sessionCookieName); err == nil {
		credential = strings.TrimSpace(cookie.Value)
	}
	if credential == "" {
		return principal, errInvalidCredential
	}
	if strings.HasPrefix(credential, "pt_") {
		token, err := s.services.Auth.Identity.APITokenByRaw(ctx, appmodel.APITokenLookupRequest{Raw: credential})
		if err != nil {
			if errors.Is(err, appmodel.ErrAuthTokenInvalid) {
				return principal, errInvalidCredential
			}
			return principal, err
		}
		if token.ReadOnly && r.Method != http.MethodGet && r.Method != http.MethodHead {
			return principal, errReadOnlyCredential
		}
		principal.teamID = token.TeamID
		principal.user, err = s.services.Auth.Identity.IdentityByID(ctx, token.UserID)
		if err != nil {
			if errors.Is(err, appmodel.ErrAuthNotFound) {
				return principal, errInvalidCredential
			}
			return principal, err
		}
		return principal, nil
	}
	user, err := s.services.Auth.Identity.AuthenticateSessionToken(ctx, credential)
	if err != nil {
		if errors.Is(err, appmodel.ErrAuthSessionInvalid) {
			principal.clearSession = true
			return principal, errInvalidCredential
		}
		return principal, err
	}
	principal.user = user
	principal.sessionToken = credential
	return principal, nil
}

// requireAuth is the actual middleware. Pulled out as a value so it
// can be composed with route-specific work (e.g. CSRF for POSTs).
//
// Behaviour:
//   - If credentials are missing or invalid → call onFailure (which
//     for pages redirects to /login, for /api/* returns 401 JSON).
//   - Otherwise attach User + Team to the context and call the next
//     handler.
//   - Touches the session so last_seen_at updates on every request.
//   - Also enforces the "current team" cookie: if it points to a team
//     the user is no longer in, fall back to their personal team.
func (s *Server) requireAuth(onFailure func(w http.ResponseWriter, r *http.Request)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			principal, err := s.authenticateRequest(ctx, r)
			if err != nil {
				if errors.Is(err, errInvalidCredential) {
					if principal.clearSession {
						// Clear the dead cookie so the browser stops sending it.
						clearSessionCookie(w)
					}
					onFailure(w, r)
				} else if errors.Is(err, errReadOnlyCredential) {
					s.writeJSONStatus(w, http.StatusForbidden, apiErrorResponse{Error: "this API token is read-only"})
				} else {
					s.writeInternalError(w, err)
				}
				return
			}
			user := principal.user
			// Keep browser-session activity current without issuing a no-op
			// session UPDATE for API-token requests.
			if principal.sessionToken != "" {
				s.services.Auth.Identity.Touch(ctx, principal.sessionToken)
			}

			var team model.Team
			var role model.TeamRole
			if principal.teamID > 0 {
				// A token acts in the workspace it was made in, while the
				// owner is still a member there.
				membership, ok, merr := s.services.Teams.Directory.MembershipForUser(ctx, principal.teamID, user.ID)
				if merr != nil {
					s.writeInternalError(w, merr)
					return
				}
				if !ok {
					onFailure(w, r)
					return
				}
				team, role = membership.Team, membership.Role
			} else {
				team, role, err = s.resolveTeam(ctx, user, r)
			}
			if err != nil {
				if errors.Is(err, errNoTeamMembership) {
					// This is an invalid account state, not a DB outage.
					onFailure(w, r)
				} else {
					s.writeInternalError(w, err)
				}
				return
			}
			// Every authenticated web request must carry an explicit workspace.
			// A zero ID is treated as unscoped by legacy persistence queries, so
			// never let an incomplete auth adapter reach a handler with it.
			if team.ID <= 0 {
				onFailure(w, r)
				return
			}
			ctx = WithUser(ctx, user)
			ctx = WithTeam(ctx, team)
			ctx = context.WithValue(ctx, ctxTeamModulesKey, &teamModulesCache{})
			ctx = context.WithValue(ctx, ctxRoleKey, role)
			ctx = requestctx.WithLocale(ctx, string(resolveLang(r)))
			ctx = requestctx.WithTeamID(ctx, team.ID)
			// Every session this request creates is the user's; a member
			// only ever sees their own time.
			ctx = requestctx.WithActor(ctx, user.ID)
			if prefs, err := s.services.Preferences.Load(ctx, user.ID); err == nil {
				ctx = withPrefs(ctx, prefs)
			} else if s.logger != nil {
				s.logger.Printf("web: load preferences for user %d: %v", user.ID, err)
			}
			if !role.CanManage() {
				ctx = requestctx.WithScope(ctx, user.ID)
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
func (s *Server) resolveTeam(ctx context.Context, user appmodel.UserIdentity, r *http.Request) (model.Team, model.TeamRole, error) {
	if cookie, err := r.Cookie(teamCookieName); err == nil {
		var wantID int64
		if _, scanErr := scanInt(cookie.Value, &wantID); scanErr == nil && wantID > 0 {
			membership, ok, err := s.services.Teams.Directory.MembershipForUser(ctx, wantID, user.ID)
			if err != nil {
				return model.Team{}, "", fmt.Errorf("load selected workspace membership: %w", err)
			}
			if ok {
				return membership.Team, membership.Role, nil
			}
		}
	}
	memberships, err := s.services.Teams.Directory.MembershipsForUser(ctx, user.ID)
	if err != nil {
		return model.Team{}, "", err
	}
	if len(memberships) == 0 {
		return model.Team{}, "", errNoTeamMembership
	}
	// MembershipsForUser orders owner workspaces first.
	return memberships[0].Team, memberships[0].Role, nil
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
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(appmodel.SessionTTL.Seconds()),
		Secure:   isSecureRequest(r),
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
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
		MaxAge:   int(appmodel.SessionTTL.Seconds()),
		Secure:   isSecureRequest(r),
	})
}

const teamCookieName = "paratrack_team"
const sessionCookieName = "paratrack_session"

type roleKey struct{}

var ctxRoleKey = roleKey{}

// RoleFrom is the user's role in the current workspace.
func RoleFrom(ctx context.Context) model.TeamRole {
	r, _ := ctx.Value(ctxRoleKey).(model.TeamRole)
	return r
}

func canManage(r *http.Request) bool { return RoleFrom(r.Context()).CanManage() }

// manage guards money and workspace settings: owner and admin only.
// Denied document GETs keep the app shell so the member can navigate away;
// APIs and mutations retain their 403 response format.
func (s *Server) manage(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if canManage(r) {
			h(w, r)
			return
		}
		msg := i18n.T(resolveLang(r), "err.managersOnly")
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get("HX-Request") == "" {
			s.writeJSONStatus(w, http.StatusForbidden, apiErrorResponse{Error: msg})
			return
		}
		if r.Method == http.MethodGet && r.Header.Get("HX-Request") == "" {
			s.renderPageForRequestStatus(w, r, http.StatusForbidden, "Access denied", "", "access-denied", &pageData{})
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
			r = r.WithContext(requestctx.WithScope(r.Context(), u.ID))
		}
		h(w, r)
	}
}
