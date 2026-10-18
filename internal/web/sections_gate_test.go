package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// The gate holds every screen until the workspace has chosen its sections, but
// leaves the choice, settings, help and invites open: a stranded account must
// always be able to reach the way out.
func TestSectionsGateOpensOnlyTheWayOut(t *testing.T) {
	open := []string{"/welcome", "/help", "/settings", "/settings/sections", "/settings/profile", "/invites/abc"}
	closed := []string{"/", "/projects", "/clients", "/settings-old", "/invitesx", "/welcomes", "/graph"}
	for _, path := range open {
		if !sectionsGateOpen(path) {
			t.Errorf("sectionsGateOpen(%q) = false, want true", path)
		}
	}
	for _, path := range closed {
		if sectionsGateOpen(path) {
			t.Errorf("sectionsGateOpen(%q) = true, want false", path)
		}
	}
}

// chooseAllSections does what the welcome screen does for a new workspace: it
// saves the studio preset, which holds every section, so the page tests see the
// whole menu as they always did. The owner's session is the one that saves it.
func chooseAllSections(t *testing.T, srv *Server, session string) {
	t.Helper()
	csrfToken, csrfCookie := seedCSRF(t, srv.routes())
	form := url.Values{"preset": {"studio"}, "from": {"welcome"}}
	r := httptest.NewRequest(http.MethodPost, "/api/team/modules", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set(csrfHeaderName, csrfToken)
	r.AddCookie(csrfCookie)
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("choose sections: want 303 to /, got %d to %q", w.Code, w.Header().Get("Location"))
	}
}

// The custom list on the welcome screen saves like a preset: it posts the ticked
// sections with from=welcome, the workspace is chosen from then on, and the menu
// holds exactly what was ticked.
func TestWelcomeCustomListSavesAndOpensTheMenu(t *testing.T) {
	e := newAPIEnv(t)
	resp := e.do("POST", "/api/register", url.Values{"name": {"U"}, "email": {"custom-welcome@x.test"}, "password": {"longenoughpw"}}, nil)
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); loc != "/welcome" {
		t.Fatalf("after sign-up: %q, want /welcome", loc)
	}
	for _, c := range resp.Cookies() {
		e.jar[c.Name] = c.Value
	}
	saved := e.do("POST", "/api/team/modules", url.Values{"modules": {"graph", "invoices"}, "from": {"welcome"}}, nil)
	saved.Body.Close()
	if loc := saved.Header.Get("Location"); loc != "/" {
		t.Fatalf("custom save goes to %q, want /", loc)
	}
	page := readBody(t, e.do("GET", "/", nil, nil))
	if !strings.Contains(page, `"invoices":true`) {
		t.Fatal("the ticked invoices section is not on after the custom save")
	}
	if strings.Contains(page, `"payroll":true`) {
		t.Fatal("an unticked section is on after the custom save")
	}
}
