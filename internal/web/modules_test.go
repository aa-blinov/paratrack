package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
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
	// Before any choice everything is on (existing workspaces keep all).
	if page := readBody(t, e.do("GET", "/", nil, nil)); !strings.Contains(page, `"invoices":true`) {
		t.Fatal("the owner's menu has no invoices")
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
