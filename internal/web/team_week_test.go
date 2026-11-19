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
	// A session recorded afterwards has ended already, so the hour is the last one before now,
	// kept inside today.
	// The server cuts days in the viewer's zone, which for a fresh test
	// account is the machine's own, so compute in that zone too: on a machine
	// ahead of UTC an evening run would otherwise write the hour onto the
	// wrong side of the local midnight and the test would flake by clock.
	now := time.Now().In(time.Local)
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	end := now.Add(-time.Minute)
	start := now.Add(-2 * time.Hour)
	if start.Before(midnight) {
		start = midnight
	}
	if !end.After(start) {
		t.Skip("the first minute of a day has no past hour to record")
	}
	saved := e.do("POST", "/api/sessions/backfill", url.Values{
		"activity": {"Week work"}, "start": {start.Format("02.01.2006 15:04")}, "end": {end.Format("02.01.2006 15:04")},
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
