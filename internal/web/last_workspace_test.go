package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// browser stands in for one browser: the cookies it keeps between
// requests. Starting empty is how a brand-new device looks, which is
// the point of the workspace memory — a browser that never held a
// cookie has to land in the right workspace too.
type browser struct {
	session string
	team    string
}

// newBrowser returns a browser with no cookies at all: a new device.
func newBrowser() *browser { return &browser{} }

// browserWith returns a browser that already carries a session, the way
// a reload of the same tab would.
func browserWith(session string) *browser {
	return &browser{session: session}
}

func (b *browser) do(t *testing.T, srv *Server, method, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	body := ""
	if form != nil {
		body = form.Encode()
	}
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	token, csrf := seedCSRF(t, srv.routes())
	r.Header.Set(csrfHeaderName, token)
	r.AddCookie(csrf)
	if b.session != "" {
		r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: b.session})
	}
	if b.team != "" {
		r.AddCookie(&http.Cookie{Name: teamCookieName, Value: b.team})
	}
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, r)
	b.remember(w)
	return w
}

// remember keeps what the response handed over, like a cookie jar.
func (b *browser) remember(w *httptest.ResponseRecorder) {
	for _, c := range w.Result().Cookies() {
		switch {
		case c.Name == sessionCookieName && c.Value != "":
			b.session = c.Value
		case c.Name == teamCookieName && c.Value != "":
			b.team = c.Value
		}
	}
}

// login signs in from scratch, standing in for a brand-new device.
func (b *browser) login(t *testing.T, srv *Server, email, password string) *httptest.ResponseRecorder {
	t.Helper()
	return b.do(t, srv, http.MethodPost, "/api/login", url.Values{
		"email": {email}, "password": {password},
	})
}

func (b *browser) logout(t *testing.T, srv *Server) *httptest.ResponseRecorder {
	t.Helper()
	w := b.do(t, srv, http.MethodPost, "/api/logout", url.Values{})
	b.session, b.team = "", ""
	return w
}

func (b *browser) createTeam(t *testing.T, srv *Server, name string) int64 {
	t.Helper()
	w := b.do(t, srv, http.MethodPost, "/api/team/create", url.Values{"name": {name}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("POST /api/team/create: want 303, got %d body=%s", w.Code, w.Body.String())
	}
	return b.currentTeam(t, srv)
}

// currentTeam reports the workspace the server scoped this browser to.
func (b *browser) currentTeam(t *testing.T, srv *Server) int64 {
	t.Helper()
	w := b.do(t, srv, http.MethodGet, "/api/me", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/me: want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var me apiMeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode /api/me: %v body=%s", err, w.Body.String())
	}
	return me.Team.ID
}

func rememberedWorkspace(t *testing.T, srv *Server, userID int64) int64 {
	t.Helper()
	prefs, err := srv.services.Preferences.Load(t.Context(), userID)
	if err != nil {
		t.Fatalf("load preferences of user %d: %v", userID, err)
	}
	return prefs.LastTeamID
}

// userIDOfSession reports whose account a session token belongs to.
func userIDOfSession(t *testing.T, srv *Server, token string) int64 {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	srv.routes().ServeHTTP(w, r)
	var me apiMeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode /api/me: %v body=%s", err, w.Body.String())
	}
	if me.User.ID <= 0 {
		t.Fatalf("GET /api/me did not resolve a user: %d %s", w.Code, w.Body.String())
	}
	return me.User.ID
}

func TestLoginReturnsToTheLastWorkspaceOnANewBrowser(t *testing.T) {
	srv, seedToken := newTestServer(t)
	alice := userIDOfSession(t, srv, seedToken)

	desk := newBrowser()
	desk.login(t, srv, "alice@example.com", "longenough")
	studio := desk.createTeam(t, srv, "Studio")
	if studio <= 0 {
		t.Fatal("creating a workspace returned no id")
	}
	if got := rememberedWorkspace(t, srv, alice); got != studio {
		t.Fatalf("choosing a workspace should remember it: want %d, got %d", studio, got)
	}

	// A phone that never held the cookie: the account has to answer.
	phone := newBrowser()
	phone.login(t, srv, "alice@example.com", "longenough")
	if got := phone.currentTeam(t, srv); got != studio {
		t.Errorf("login on a fresh browser: want workspace %d, got %d", studio, got)
	}
}

func TestLogoutThenLoginStillReturnsToTheLastWorkspace(t *testing.T) {
	srv, seedToken := newTestServer(t)
	alice := userIDOfSession(t, srv, seedToken)

	desk := newBrowser()
	desk.login(t, srv, "alice@example.com", "longenough")
	studio := desk.createTeam(t, srv, "Studio")
	desk.logout(t, srv)
	desk.login(t, srv, "alice@example.com", "longenough")

	if got := desk.currentTeam(t, srv); got != studio {
		t.Errorf("after logout and a new login: want workspace %d, got %d", studio, got)
	}
	if got := rememberedWorkspace(t, srv, alice); got != studio {
		t.Errorf("logout must not forget the workspace: want %d, got %d", studio, got)
	}
}

func TestLogoutClearsTheWorkspaceCookie(t *testing.T) {
	srv, _ := newTestServer(t)
	desk := newBrowser()
	desk.login(t, srv, "alice@example.com", "longenough")
	desk.createTeam(t, srv, "Studio")
	if desk.team == "" {
		t.Fatal("choosing a workspace should leave a team cookie")
	}

	w := desk.logout(t, srv)
	cleared := false
	for _, c := range w.Result().Cookies() {
		if c.Name == teamCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Errorf("POST /api/logout should clear %s, got %v", teamCookieName, w.Result().Cookies())
	}
}

func TestANewAccountLogsIntoItsOnlyWorkspace(t *testing.T) {
	srv, _ := newTestServer(t)
	bob := userIDOfSession(t, srv, secondUser(t, srv, "bob@example.com", "Bob"))

	phone := newBrowser()
	phone.login(t, srv, "bob@example.com", "longenough")
	if phone.team != "" {
		t.Errorf("an account with one workspace needs no cookie, got %s", phone.team)
	}
	if phone.currentTeam(t, srv) == 0 {
		t.Error("login left the account without a workspace")
	}
	if got := rememberedWorkspace(t, srv, bob); got != 0 {
		t.Errorf("nothing was chosen yet, so nothing should be remembered, got %d", got)
	}
}

func TestAnotherAccountDoesNotInheritTheWorkspaceCookie(t *testing.T) {
	srv, _ := newTestServer(t)
	bobToken := secondUser(t, srv, "bob@example.com", "Bob")
	bob := userIDOfSession(t, srv, bobToken)

	desk := newBrowser()
	desk.login(t, srv, "alice@example.com", "longenough")
	studio := desk.createTeam(t, srv, "Studio")
	desk.logout(t, srv)

	// Same browser, next person. Bob is not in Alice's studio, so the
	// pointer Alice left behind must not carry him there.
	desk.login(t, srv, "bob@example.com", "longenough")
	if got := desk.currentTeam(t, srv); got == studio {
		t.Errorf("Bob landed in Alice's workspace %d", studio)
	}
	if got := rememberedWorkspace(t, srv, bob); got == studio {
		t.Errorf("Bob's account remembers Alice's workspace %d", studio)
	}
}

func TestAWorkspaceCookieCannotGrantMembership(t *testing.T) {
	srv, _ := newTestServer(t)
	alice := newBrowser()
	alice.login(t, srv, "alice@example.com", "longenough")
	studio := alice.createTeam(t, srv, "Studio")
	bobToken := secondUser(t, srv, "bob@example.com", "Bob")

	// Bob forges the pointer Alice's browser carries.
	forged := browserWith(bobToken)
	forged.team = intToString(studio)
	if got := forged.currentTeam(t, srv); got == studio {
		t.Errorf("a forged %s cookie put Bob into workspace %d", teamCookieName, studio)
	}
}

func TestJoiningAStudioIsRememberedForTheNextLogin(t *testing.T) {
	srv, ownerToken := newTestServer(t)
	owner := browserWith(ownerToken)

	w := owner.do(t, srv, http.MethodPost, "/api/team/invites", nil)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("POST /api/team/invites: want 303, got %d body=%s", w.Code, w.Body.String())
	}
	page := owner.do(t, srv, http.MethodGet, "/settings/invites", nil)
	if page.Code != http.StatusOK {
		t.Fatalf("GET /settings/invites: want 200, got %d", page.Code)
	}
	invites := reactData[settingsPageData](t, page.Body.String()).Invites
	if len(invites) != 1 || invites[0].Token == "" {
		t.Fatal("invite bootstrap lacks a token")
	}

	bobToken := secondUser(t, srv, "bob@example.com", "Bob")
	bob := userIDOfSession(t, srv, bobToken)
	bobBrowser := browserWith(bobToken)
	accept := bobBrowser.do(t, srv, http.MethodPost, "/api/invites/"+invites[0].Token+"/accept", nil)
	if accept.Code != http.StatusSeeOther {
		t.Fatalf("POST invite accept: want 303, got %d body=%s", accept.Code, accept.Body.String())
	}
	studio := bobBrowser.currentTeam(t, srv)
	if studio == 0 {
		t.Fatal("accepting an invite left the account without a workspace")
	}
	if got := rememberedWorkspace(t, srv, bob); got != studio {
		t.Errorf("the joined workspace should be remembered: want %d, got %d", studio, got)
	}

	// And from a device that never saw the invite.
	laptop := newBrowser()
	laptop.login(t, srv, "bob@example.com", "longenough")
	if got := laptop.currentTeam(t, srv); got != studio {
		t.Errorf("login after joining a studio: want workspace %d, got %d", studio, got)
	}
}
