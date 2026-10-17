package web

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

func TestSectionsOnboardingAndSettings(t *testing.T) {
	e := newAPIEnv(t)
	resp := e.do("POST", "/api/register", url.Values{"name": {"U"}, "email": {"mods@x.test"}, "password": {"longenoughpw"}}, nil)
	resp.Body.Close()
	if loc := resp.Header.Get("Location"); loc != "/welcome" {
		t.Fatalf("after sign-up: %q, want /welcome", loc)
	}
	for _, c := range resp.Cookies() {
		e.jar[c.Name] = c.Value
	}
	// Before the choice the menu waits at /welcome: a new workspace opens with the
	// sections its owner picks, not with everything switched on.
	if wait := e.do("GET", "/", nil, nil); wait.Header.Get("Location") != "/welcome" {
		t.Fatalf("before the choice: GET / goes to %q, want /welcome", wait.Header.Get("Location"))
	} else {
		wait.Body.Close()
	}
	if w := readBody(t, e.do("GET", "/welcome", nil, nil)); !reactData[sectionsPage](t, w).Welcome || len(reactData[sectionsPage](t, w).Presets) != 3 {
		t.Fatal("welcome has no presets")
	}

	// "Just me": no invoices, payroll, schedule; graph stays.
	resp = e.do("POST", "/api/team/modules", url.Values{"preset": {"solo"}, "from": {"welcome"}}, nil)
	resp.Body.Close()
	if resp.Header.Get("Location") != "/" {
		t.Errorf("welcome save goes to %q", resp.Header.Get("Location"))
	}
	page := readBody(t, e.do("GET", "/", nil, nil))
	if mode := reactData[dashboardData](t, page).Mode; mode != "solo" {
		t.Fatalf("dashboard mode = %q, want solo", mode)
	}
	for _, off := range []string{`"invoices":false`, `"payroll":false`, `"schedule":false`} {
		if strings.Contains(page, off) {
			t.Errorf("solo menu still links %s", off)
		}
	}
	if !strings.Contains(page, `"graph":true`) {
		t.Error("solo menu lost the by-hour graph")
	}
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("GET", e.ts.URL+"/invoices", nil)
	for k, v := range e.jar {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	r2, _ := noFollow.Do(req)
	r2.Body.Close()
	if r2.StatusCode != 303 || !strings.HasPrefix(r2.Header.Get("Location"), "/settings/sections") {
		t.Errorf("switched-off invoices: %d %q", r2.StatusCode, r2.Header.Get("Location"))
	}

	// Hand-picked: just invoices.
	resp = e.do("POST", "/api/team/modules", url.Values{"modules": {"invoices"}}, nil)
	resp.Body.Close()
	page = readBody(t, e.do("GET", "/", nil, nil))
	if !strings.Contains(page, `"invoices":true`) || strings.Contains(page, `href="/graph"`) {
		t.Error("custom set not applied")
	}
	if mode := reactData[dashboardData](t, page).Mode; mode != "custom" {
		t.Fatalf("dashboard mode = %q, want custom", mode)
	}
	// Nothing ticked is a real choice (core only), not "everything".
	resp = e.do("POST", "/api/team/modules", url.Values{}, nil)
	resp.Body.Close()
	if page := readBody(t, e.do("GET", "/", nil, nil)); strings.Contains(page, `"invoices":true`) {
		t.Error("an all-off choice fell back to everything")
	}
}

func TestPresetKeyForModules(t *testing.T) {
	for _, preset := range presets {
		selected := map[string]bool{}
		for _, key := range preset.Modules {
			selected[key] = true
		}
		if got := presetKeyForModules(selected); got != preset.Key {
			t.Errorf("preset %s resolved to %s", preset.Key, got)
		}
	}
	for _, selected := range []map[string]bool{nil, {}, {"invoices": true}, {"schedule": true}} {
		if got := presetKeyForModules(selected); got != "custom" {
			t.Errorf("custom set %+v resolved to %s", selected, got)
		}
	}
}

// The journal names what a sections save switched on or off, and a save that
// changes nothing writes no entry.
func TestSectionSaveAuditsOnlyChanges(t *testing.T) {
	e := newAPIEnv(t)
	e.register("audit-sections@x.test")
	ctx := t.Context()
	var teamID int64
	if err := e.db.TestSQL().QueryRowContext(ctx, `SELECT id FROM teams ORDER BY id LIMIT 1`).Scan(&teamID); err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	everything := []string{"graph", "goals", "tags", "invoices", "reports", "payroll", "schedule", "integrations", "import"}
	withoutTags := []string{"graph", "goals", "invoices", "reports", "payroll", "schedule", "integrations", "import"}
	save := func(modules []string) string {
		t.Helper()
		resp := e.do("POST", "/api/team/modules", url.Values{"modules": modules}, nil)
		resp.Body.Close()
		return resp.Header.Get("Location")
	}
	journal := func() []string {
		t.Helper()
		entries, err := e.srv.services.AuditLog.List(ctx, appmodel.AuditListQuery{TeamID: teamID, Action: "team.modules", Limit: 50})
		if err != nil {
			t.Fatalf("list audit: %v", err)
		}
		targets := make([]string, 0, len(entries))
		for _, entry := range entries {
			targets = append(targets, entry.Target)
		}
		slices.Sort(targets)
		return targets
	}

	// A new workspace shows every section, so saving that same set changes nothing.
	if loc := save(everything); loc != "/settings/sections?flash=updated" {
		t.Fatalf("no-op save redirects to %q", loc)
	}
	if got := journal(); len(got) != 0 {
		t.Fatalf("no-op save wrote %v", got)
	}

	if loc := save(withoutTags); loc != "/settings/sections?flash=updated" {
		t.Fatalf("save redirects to %q", loc)
	}
	if got := journal(); !slices.Equal(got, []string{"-tags"}) {
		t.Fatalf("switching tags off journaled %v, want [-tags]", got)
	}

	// Saving the same set again is a no-op: still one entry.
	save(withoutTags)
	if got := journal(); !slices.Equal(got, []string{"-tags"}) {
		t.Fatalf("repeat save journaled %v, want still [-tags]", got)
	}

	save(everything)
	if got := journal(); !slices.Equal(got, []string{"+tags", "-tags"}) {
		t.Fatalf("switching tags back on journaled %v, want [+tags -tags]", got)
	}
}
