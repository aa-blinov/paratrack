package web

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

type credentialIdentityStub struct {
	user       appmodel.UserIdentity
	touchCalls []string
}

func (s *credentialIdentityStub) APITokenByRaw(context.Context, appmodel.APITokenLookupRequest) (appmodel.APITokenIdentity, error) {
	return appmodel.APITokenIdentity{UserID: s.user.ID, TeamID: 9}, nil
}

func (s *credentialIdentityStub) Logout(context.Context, appmodel.LogoutRequest) error { return nil }

func (s *credentialIdentityStub) IdentityByID(context.Context, int64) (appmodel.UserIdentity, error) {
	return s.user, nil
}

func (s *credentialIdentityStub) AuthenticateSessionToken(context.Context, string) (appmodel.UserIdentity, error) {
	return s.user, nil
}

func (s *credentialIdentityStub) Touch(_ context.Context, token string) {
	s.touchCalls = append(s.touchCalls, token)
}

type credentialTeamDirectoryStub struct{ membership model.TeamMembership }

func (s credentialTeamDirectoryStub) FindByID(context.Context, int64) (model.Team, error) {
	return s.membership.Team, nil
}

func (s credentialTeamDirectoryStub) FindByIDs(context.Context, []int64) (map[int64]model.Team, error) {
	return map[int64]model.Team{s.membership.Team.ID: s.membership.Team}, nil
}

func (s credentialTeamDirectoryStub) IsMember(context.Context, appmodel.TeamMembershipQuery) (model.TeamRole, bool, error) {
	return s.membership.Role, true, nil
}

func (s credentialTeamDirectoryStub) Members(context.Context, int64) ([]model.TeamMember, error) {
	return nil, nil
}

func (s credentialTeamDirectoryStub) MembershipForUser(context.Context, appmodel.TeamMembershipQuery) (model.TeamMembership, bool, error) {
	return s.membership, true, nil
}

func (s credentialTeamDirectoryStub) MembershipsForUser(context.Context, int64) ([]model.TeamMembership, error) {
	return []model.TeamMembership{s.membership}, nil
}

type credentialPreferencesStub struct{}

func (credentialPreferencesStub) Load(context.Context, int64) (appmodel.UserPreferences, error) {
	return appmodel.UserPreferences{}, nil
}

func (credentialPreferencesStub) Save(context.Context, appmodel.PreferencesSaveRequest) error {
	return nil
}

type failingCredentialPreferencesStub struct{ err error }

func (s failingCredentialPreferencesStub) Load(context.Context, int64) (appmodel.UserPreferences, error) {
	return appmodel.UserPreferences{}, s.err
}

func (failingCredentialPreferencesStub) Save(context.Context, appmodel.PreferencesSaveRequest) error {
	return nil
}

func TestRequireAuthTouchesOnlyBrowserSessions(t *testing.T) {
	for _, test := range []struct {
		name       string
		credential string
		wantTouch  string
	}{
		{name: "API token", credential: "pt_api", wantTouch: ""},
		{name: "browser session", credential: "browser-session", wantTouch: "browser-session"},
	} {
		t.Run(test.name, func(t *testing.T) {
			identity := &credentialIdentityStub{user: appmodel.UserIdentity{ID: 7}}
			membership := model.TeamMembership{
				Team: model.Team{ID: 9}, Role: model.TeamRoleMember,
			}
			server := &Server{services: Dependencies{
				Auth:        AuthenticationDependencies{Identity: identity},
				Teams:       TeamDependencies{Directory: credentialTeamDirectoryStub{membership: membership}},
				Preferences: credentialPreferencesStub{},
			}}
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if test.name == "API token" {
				req.Header.Set("Authorization", "Bearer "+test.credential)
			} else {
				req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: test.credential})
			}
			recorder := httptest.NewRecorder()
			called := false
			handler := server.requireAuth(func(http.ResponseWriter, *http.Request) {})
			handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(recorder, req)
			if !called {
				t.Fatalf("authenticated request did not reach the next handler: status %d", recorder.Code)
			}
			if len(identity.touchCalls) != 0 && len(identity.touchCalls) != 1 {
				t.Fatalf("Touch called %d times, want at most once", len(identity.touchCalls))
			}
			if test.wantTouch == "" && len(identity.touchCalls) != 0 {
				t.Fatalf("API token unexpectedly touched browser session %q", identity.touchCalls[0])
			}
			if test.wantTouch != "" && (len(identity.touchCalls) != 1 || identity.touchCalls[0] != test.wantTouch) {
				t.Fatalf("Touch calls = %v, want [%q]", identity.touchCalls, test.wantTouch)
			}
		})
	}
}

func TestRequireAuthLogsPreferenceLoadFailureAndContinues(t *testing.T) {
	identity := &credentialIdentityStub{user: appmodel.UserIdentity{ID: 7}}
	membership := model.TeamMembership{
		Team: model.Team{ID: 9}, Role: model.TeamRoleMember,
	}
	var logs bytes.Buffer
	server := &Server{
		logger: log.New(&logs, "", 0),
		services: Dependencies{
			Auth:        AuthenticationDependencies{Identity: identity},
			Teams:       TeamDependencies{Directory: credentialTeamDirectoryStub{membership: membership}},
			Preferences: failingCredentialPreferencesStub{err: errors.New("preferences store unavailable")},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "browser-session"})
	recorder := httptest.NewRecorder()
	called := false
	server.requireAuth(func(http.ResponseWriter, *http.Request) {})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(recorder, req)

	if !called {
		t.Fatalf("request did not continue after preference failure: status %d", recorder.Code)
	}
	if got := logs.String(); !strings.Contains(got, "load preferences for user 7") || !strings.Contains(got, "preferences store unavailable") {
		t.Fatalf("preference failure log = %q", got)
	}
}
