package web

import (
	"net/url"
	"testing"
	"time"
)

// A manager's statistics page carries the team's week: a row per member and seven
// days, with today's time in the viewer's own column (PLAN-04).
func TestManagerStatsCarriesTheTeamWeek(t *testing.T) {
	e := newAPIEnv(t)
	e.register("team-week@x.test")
	today := time.Now().UTC().Format("02.01.2006")
	saved := e.do("POST", "/api/sessions/backfill", url.Values{
		"activity": {"Week work"}, "start": {today + " 09:00"}, "end": {today + " 10:00"},
	}, nil)
	readBody(t, saved)
	if saved.Header.Get("X-Backfill-Saved") != "true" {
		t.Fatalf("the hour was not saved: status %d", saved.StatusCode)
	}

	page := reactData[statsData](t, readBody(t, e.do("GET", "/stats", nil, nil)))
	if page.TeamWeek == nil || len(page.TeamWeek.Days) != 7 || len(page.TeamWeek.Rows) == 0 {
		t.Fatalf("the statistics page carries no team week: %+v", page.TeamWeek)
	}
	todayIndex := -1
	for i, day := range page.TeamWeek.Days {
		if day.Today {
			todayIndex = i
		}
	}
	if todayIndex < 0 {
		t.Fatal("no day of the team week is marked as today")
	}
	if cell := page.TeamWeek.Rows[0].Cells[todayIndex]; cell.Label == "" {
		t.Fatalf("today's hour is missing from the team week: %+v", page.TeamWeek.Rows[0])
	}
}
