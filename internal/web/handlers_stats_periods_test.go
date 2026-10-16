package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// An empty /stats period has to stay reachable. Today is empty on most days
// (the default is the week), so yesterday's hours live outside it — an empty
// page that names no other period reads as "nothing was ever tracked", and
// the reader has no way to tell otherwise.
func TestStatsEmptyPeriodOffersPeriodsThatHoldTime(t *testing.T) {
	e := newAPIEnv(t)
	e.register("empty-period@x.test")

	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	resp := e.do("POST", "/api/sessions/backfill", url.Values{
		"activity": {"reading"}, "start": {yesterday + " 09:00"}, "end": {yesterday + " 09:45"},
	}, map[string]string{"HX-Request": "true"})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("backfill yesterday: %d %s", resp.StatusCode, readBody(t, resp))
	}
	resp.Body.Close()

	if def := reactData[statsData](t, readBody(t, e.do("GET", "/stats", nil, nil))); def.Period.Label != "week" {
		t.Fatalf("default /stats period = %q, want week", def.Period.Label)
	}
	data := reactData[statsData](t, readBody(t, e.do("GET", "/stats?period=today", nil, nil)))
	if len(data.Sessions) != 0 {
		t.Fatalf("today holds %d sessions, want none", len(data.Sessions))
	}
	offered := map[string]statsPeriodOption{}
	for _, option := range data.Elsewhere {
		offered[option.Label] = option
	}
	if option, ok := offered["yesterday"]; !ok || option.Count != 1 || option.Total == "" {
		t.Fatalf("empty today must offer yesterday's one session, got %+v", data.Elsewhere)
	}
	if _, ok := offered["today"]; ok {
		t.Errorf("the empty period itself must not be offered as elsewhere: %+v", data.Elsewhere)
	}

	// The period the empty state names has to lead to the session it counted.
	linked := reactData[statsData](t, readBody(t, e.do("GET", "/stats?period=yesterday", nil, nil)))
	if len(linked.Sessions) != 1 {
		t.Fatalf("yesterday view shows %d sessions, want 1", len(linked.Sessions))
	}
	if len(linked.Elsewhere) != 0 {
		t.Errorf("a period that holds sessions needs no elsewhere hint, got %+v", linked.Elsewhere)
	}
}

// Stats totals count closed sessions only, so a running timer used to make
// /stats disagree with the dashboard without a word. The page names it.
func TestStatsNamesRunningTimers(t *testing.T) {
	e := newAPIEnv(t)
	e.register("running-stats@x.test")

	resp := e.do("POST", "/api/start", url.Values{"activity": {"reading"}}, map[string]string{"HX-Request": "true"})
	if resp.StatusCode >= 400 {
		t.Fatalf("start: %d %s", resp.StatusCode, readBody(t, resp))
	}
	resp.Body.Close()

	data := reactData[statsData](t, readBody(t, e.do("GET", "/stats", nil, nil)))
	if data.RunningCount != 1 || data.RunningTotal == "" {
		t.Fatalf("running timer not named: count %d total %q", data.RunningCount, data.RunningTotal)
	}
	tagged := reactData[statsData](t, readBody(t, e.do("GET", "/stats?tag=none", nil, nil)))
	if tagged.RunningCount != 0 {
		t.Errorf("a tag filter cannot vouch for untagged timers, got %d", tagged.RunningCount)
	}
}

// A page that does not exist is still a screen of the app: /invoices/new used
// to answer a bare "404 page not found" with no menu and no way back. JSON and
// HTMX callers keep the plain status.
func TestPageNotFoundRendersInsideApp(t *testing.T) {
	e := newAPIEnv(t)
	e.register("not-found@x.test")

	for _, path := range []string{"/invoices/new", "/no-such-page"} {
		resp := e.do("GET", path, nil, map[string]string{"Accept": "text/html"})
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: status %d, want 404", path, resp.StatusCode)
		}
		data := reactData[accessDeniedPage](t, body)
		if data.Heading == "" || data.Message == "" {
			t.Fatalf("%s: the app's 404 screen is missing: %+v", path, data)
		}
	}
	resp := e.do("GET", "/invoices/new", nil, map[string]string{"Accept": "text/html", "HX-Request": "true"})
	if body := readBody(t, resp); resp.StatusCode != http.StatusNotFound || strings.Contains(body, "<html") {
		t.Errorf("an HTMX swap keeps the plain 404, got %d", resp.StatusCode)
	}
}
