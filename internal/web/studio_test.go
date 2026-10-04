package web

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// A studio: owner + developer in one workspace.
func TestStudioRolesAndIsolation(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("owner@studio.test")
	dev := newAPIEnvSharedDB(t, owner)
	dev.register("dev@studio.test")
	htmx := map[string]string{"HX-Request": "true"}

	// Owner makes a shared workspace and invites the developer.
	resp := owner.do("POST", "/api/team/create", url.Values{"name": {"Студия"}}, nil)
	resp.Body.Close()
	for _, c := range resp.Cookies() {
		owner.jar[c.Name] = c.Value
	}
	resp = owner.do("POST", "/api/team/invites", nil, nil)
	loc, _ := url.QueryUnescape(resp.Header.Get("Location"))
	resp.Body.Close()
	token := regexp.MustCompile(`/invites/([A-Za-z0-9_-]+)`).FindStringSubmatch(loc)
	if token == nil {
		t.Fatalf("no invite link in %q", loc)
	}
	resp = dev.do("POST", "/api/invites/"+token[1]+"/accept", nil, nil)
	resp.Body.Close()
	for _, c := range resp.Cookies() {
		dev.jar[c.Name] = c.Value
	}

	// Both run a timer; each sees only their own.
	readBody(t, owner.do("POST", "/api/start", url.Values{"activity": {"ревью"}}, htmx))
	devList := readBody(t, dev.do("POST", "/api/start", url.Values{"activity": {"вёрстка"}}, htmx))
	if strings.Contains(devList, "ревью") {
		t.Fatal("developer sees the owner's timer")
	}
	ownerList := readBody(t, owner.do("GET", "/api/active", nil, htmx))
	if strings.Contains(ownerList, "вёрстка") {
		t.Fatal("owner's dashboard shows the developer's timer")
	}
	ownerID := regexp.MustCompile(`sessions/(\d+)/stop`).FindStringSubmatch(ownerList)[1]
	resp = dev.do("POST", "/api/sessions/"+ownerID+"/stop", nil, htmx)
	resp.Body.Close()
	if resp.StatusCode == 200 {
		t.Fatal("developer stopped the owner's timer")
	}
	// Nor rewrite it (sequential ids are easy to guess).
	for _, path := range []string{"/api/sessions/" + ownerID, "/api/v1/sessions/" + ownerID} {
		resp = dev.do("PATCH", path, url.Values{"note": {"взлом"}, "start_at": {"2026-09-01T10:00"}, "end_at": {"2026-09-01T11:00"}}, htmx)
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Errorf("developer PATCH %s: %d, want 404", path, resp.StatusCode)
		}
	}
	oid, _ := strconv.ParseInt(ownerID, 10, 64)
	if got, _ := owner.db.GetSession(t.Context(), appmodel.SessionLookupQuery{SessionID: oid}); (got.Note != nil && *got.Note == "взлом") || got.EndAt != nil {
		t.Fatalf("developer rewrote the owner's session: %+v", got)
	}
	readBody(t, dev.do("POST", "/api/active/stop-all", nil, htmx))
	if !strings.Contains(readBody(t, owner.do("GET", "/api/active", nil, htmx)), "ревью") {
		t.Fatal("developer's stop-all stopped the owner's timer")
	}

	// Money and settings are for managers.
	for _, path := range []string{"/invoices", "/payroll", "/settings/team", "/settings/members", "/reports"} {
		resp := dev.do("GET", path, nil, nil)
		body := readBody(t, resp)
		if resp.StatusCode != 403 {
			t.Errorf("developer GET %s: %d, want 403", path, resp.StatusCode)
		}
		if !strings.Contains(body, `<main id="main"`) || !strings.Contains(body, `href="/" class="btn btn-neutral btn-sm"`) || !strings.Contains(body, "Это доступно владельцу") {
			t.Errorf("developer GET %s: denial must explain access and offer a way back", path)
		}
	}
	resp = dev.do("POST", "/api/team/currency", url.Values{"currency": {"USD"}}, nil)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("developer changed the currency: %d", resp.StatusCode)
	}
	if page := readBody(t, dev.do("GET", "/", nil, nil)); strings.Contains(page, `href="/invoices"`) {
		t.Error("developer's menu shows invoices")
	}
	if page := readBody(t, owner.do("GET", "/", nil, nil)); !strings.Contains(page, `href="/invoices"`) || !strings.Contains(page, `href="/settings/team"`) {
		t.Error("the owner's menu lost invoices or team settings")
	}

	// Timesheet: the developer's cell doesn't wipe the owner's day.
	readBody(t, owner.do("POST", "/api/active/stop-all", nil, htmx))
	var actID, day string
	owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT activity_id, substr(start_at, 1, 10) FROM sessions WHERE id = ?`, ownerID).Scan(&actID, &day)
	readBody(t, dev.do("POST", "/api/timesheet/cell", url.Values{"activity_id": {actID}, "date": {day}, "minutes": {"30"}}, htmx))
	var n int
	owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT count(*) FROM sessions WHERE id = ?`, ownerID).Scan(&n)
	if n != 1 {
		t.Fatal("a developer's timesheet cell deleted the owner's session")
	}

	// Payroll pays each person their own time.
	var nobody int
	owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT count(*) FROM sessions WHERE user_id IS NULL`).Scan(&nobody)
	if nobody != 0 {
		t.Errorf("%d sessions without an author", nobody)
	}

	// Only the owner hands out roles.
	var devID string
	owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM users WHERE email = 'dev@studio.test'`).Scan(&devID)
	resp = dev.do("POST", "/api/team/members/"+devID+"/role", url.Values{"role": {"admin"}}, nil)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("developer made themselves admin: %d", resp.StatusCode)
	}
	resp = owner.do("POST", "/api/team/members/"+devID+"/role", url.Values{"role": {"admin"}}, nil)
	resp.Body.Close()
	if resp := dev.do("GET", "/invoices", nil, nil); resp.StatusCode != 200 {
		t.Errorf("admin can't open invoices: %d", resp.StatusCode)
	}
}

// Pages and actions that parse an id out of the path.
func TestInvitesPageAndPathIDs(t *testing.T) {
	e := newAPIEnv(t)
	e.register("paths@x.test")
	resp := e.do("POST", "/api/team/invites", nil, nil)
	resp.Body.Close()
	if resp := e.do("GET", "/settings/invites", nil, nil); resp.StatusCode != 200 {
		t.Fatalf("invites page with an invite: %d", resp.StatusCode)
	}
	resp = e.do("POST", "/api/tokens", url.Values{"name": {"x"}}, nil)
	resp.Body.Close()
	resp = e.do("POST", "/api/tokens/1/delete", nil, nil)
	resp.Body.Close()
	if resp.StatusCode != 303 {
		t.Errorf("token delete: %d", resp.StatusCode)
	}
	resp = e.do("POST", "/api/webhooks", url.Values{"url": {"https://example.com/h"}, "secret": {"s"}, "events": {"*"}}, nil)
	resp.Body.Close()
	resp = e.do("POST", "/api/webhooks/1/delete", nil, nil)
	resp.Body.Close()
	if resp.StatusCode != 303 {
		t.Errorf("webhook delete: %d", resp.StatusCode)
	}
}

// "вчера 10:00" means 10:00 where the user is, not on the UTC server.
func TestBackfillInUserZone(t *testing.T) {
	e := newAPIEnv(t)
	e.register("tz@x.test")
	e.jar["paratrack_tz"] = "Europe/Moscow"
	readBody(t, e.do("POST", "/api/sessions/backfill", url.Values{"activity": {"a"}, "start": {"вчера 10:00"}, "end": {"вчера 11:00"}}, map[string]string{"HX-Request": "true"}))
	var start string
	e.db.TestSQL().QueryRowContext(t.Context(), `SELECT start_at FROM sessions ORDER BY id DESC LIMIT 1`).Scan(&start)
	if !strings.Contains(start, "T07:00:00") {
		t.Fatalf("stored %s, want 07:00 UTC (10:00 MSK)", start)
	}
}
