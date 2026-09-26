package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/auth"
)

// secondUser creates a second user via the auth service and returns
// a fresh session token. Used to test multi-user flows (owner invites
// a member).
func secondUser(t *testing.T, srv *Server, email, name string) string {
	t.Helper()
	uid, _, err := srv.auth.CreateUser(context.Background(), email, "longenough", name)
	if err != nil {
		t.Fatalf("seed %s: %v", email, err)
	}
	sess, err := srv.auth.NewSession(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	return sess.Token
}

func TestSettingsPagesAccessibleToLoggedInUser(t *testing.T) {
	srv, token := newTestServer(t)
	for _, path := range []string{"/settings/team", "/settings/members", "/settings/invites", "/settings/profile"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		srv.routes().ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s: want 200, got %d body=%q", path, w.Code,
				w.Body.String()[:min(200, len(w.Body.String()))])
		}
	}
}

func TestSettingsPagesRedirectAnonymous(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, path := range []string{"/settings/team", "/settings/members", "/settings/invites", "/settings/profile"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		srv.routes().ServeHTTP(w, r)
		if w.Code != http.StatusSeeOther {
			t.Errorf("GET %s without cookie: want 303, got %d", path, w.Code)
		}
	}
}

func TestTeamSettingsPageShowsCurrentTeamName(t *testing.T) {
	srv, token := newTestServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/settings/team", nil)
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	srv.routes().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /settings/team: want 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Alice") {
		t.Errorf("body should contain the auto-created personal team name, got first 1000 chars: %q",
			w.Body.String()[:min(1000, len(w.Body.String()))])
	}
}

func TestCreateTeamAndSwitch(t *testing.T) {
	srv, token := newTestServer(t)

	// Create a second team as Alice.
	w := httptest.NewRecorder()
	form := strings.NewReader("name=Side+Project")
	r := httptest.NewRequest(http.MethodPost, "/api/team/create", form)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	csrfTok, csrfCk := seedCSRF(t, srv.routes())
	r.Header.Set(csrfHeaderName, csrfTok)
	r.AddCookie(csrfCk)
	srv.routes().ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("POST /api/team/create: want 303, got %d body=%q", w.Code,
			w.Body.String()[:min(200, len(w.Body.String()))])
	}

	// After creating, /settings/team should list two workspaces.
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/settings/team", nil)
	r2.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	srv.routes().ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("GET /settings/team after create: %d", w2.Code)
	}
	if !strings.Contains(w2.Body.String(), "Side Project") {
		t.Errorf("body should mention the new team, got: %q",
			w2.Body.String()[:min(500, len(w2.Body.String()))])
	}
}

func TestInviteFlowEndToEnd(t *testing.T) {
	srv, ownerToken := newTestServer(t)

	// Owner generates an invite.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/team/invites", nil)
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: ownerToken})
	csrfTok, csrfCk := seedCSRF(t, srv.routes())
	r.Header.Set(csrfHeaderName, csrfTok)
	r.AddCookie(csrfCk)
	srv.routes().ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("POST /api/team/invites: want 303, got %d", w.Code)
	}

	// Find the freshly-minted token. We pull it from the DB rather
	// than parsing the flash message — simpler and more durable.
	teams := srv.teams
	owner, err := srv.auth.FindByEmail(context.Background(), "alice@example.com")
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	ownerTeams, err := teams.ListForUser(context.Background(), owner.ID)
	if err != nil || len(ownerTeams) == 0 {
		t.Fatalf("ListForUser: %v / %d teams", err, len(ownerTeams))
	}
	invites, err := teams.InvitesForTeam(context.Background(), ownerTeams[0].ID)
	if err != nil || len(invites) == 0 {
		t.Fatalf("InvitesForTeam: %v / %d", err, len(invites))
	}
	token := invites[0].Token

	// Member tries to view the invite-accept page.
	memberToken := secondUser(t, srv, "bob@example.com", "Bob")
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/invites/"+token, nil)
	r2.AddCookie(&http.Cookie{Name: auth.CookieName, Value: memberToken})
	srv.routes().ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("GET /invites/%s: want 200, got %d body=%s", token, w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), "Alice") {
		t.Errorf("body should mention joining Alice, got first 300 chars: %q",
			w2.Body.String()[:min(300, len(w2.Body.String()))])
	}

	// Member accepts.
	w3 := httptest.NewRecorder()
	r3 := httptest.NewRequest(http.MethodPost, "/api/invites/"+token+"/accept", nil)
	r3.AddCookie(&http.Cookie{Name: auth.CookieName, Value: memberToken})
	csrfTok, csrfCk = seedCSRF(t, srv.routes())
	r3.Header.Set(csrfHeaderName, csrfTok)
	r3.AddCookie(csrfCk)
	srv.routes().ServeHTTP(w3, r3)
	if w3.Code != http.StatusSeeOther {
		t.Fatalf("POST /api/invites/.../accept: want 303, got %d", w3.Code)
	}

	// Owner /settings/members should now list Bob.
	w4 := httptest.NewRecorder()
	r4 := httptest.NewRequest(http.MethodGet, "/settings/members", nil)
	r4.AddCookie(&http.Cookie{Name: auth.CookieName, Value: ownerToken})
	srv.routes().ServeHTTP(w4, r4)
	if w4.Code != http.StatusOK {
		t.Fatalf("GET /settings/members: %d", w4.Code)
	}
	if !strings.Contains(w4.Body.String(), "Bob") {
		t.Errorf("members list should mention Bob, got: %q",
			w4.Body.String()[:min(500, len(w4.Body.String()))])
	}
}

func TestMemberCannotRemoveOtherMembers(t *testing.T) {
	srv, _ := newTestServer(t)
	memberToken := secondUser(t, srv, "bob@example.com", "Bob")

	// Bob isn't in Alice's team yet. Trying to remove himself from
	// her team should be refused.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/team/members/1/remove", nil)
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: memberToken})
	csrfTok, csrfCk := seedCSRF(t, srv.routes())
	r.Header.Set(csrfHeaderName, csrfTok)
	r.AddCookie(csrfCk)
	srv.routes().ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("non-member remove attempt: want 303 with error, got %d", w.Code)
	}
}

func TestProfileUpdate(t *testing.T) {
	srv, token := newTestServer(t)

	w := httptest.NewRecorder()
	form := strings.NewReader("name=Alice+Updated")
	r := httptest.NewRequest(http.MethodPost, "/api/profile", form)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	csrfTok, csrfCk := seedCSRF(t, srv.routes())
	r.Header.Set(csrfHeaderName, csrfTok)
	r.AddCookie(csrfCk)
	srv.routes().ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("POST /api/profile: want 303, got %d", w.Code)
	}

	// Reload profile page; it should now reflect the new name.
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/settings/profile", nil)
	r2.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	srv.routes().ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("GET /settings/profile: %d", w2.Code)
	}
	if !strings.Contains(w2.Body.String(), "Alice Updated") {
		t.Errorf("profile page should reflect the new name, got: %q",
			w2.Body.String()[:min(500, len(w2.Body.String()))])
	}
}