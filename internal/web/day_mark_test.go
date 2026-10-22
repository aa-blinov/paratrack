package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// A member marks today as overloaded: the mark is listed on their own week, and the
// manager's team week shows it on their day (TIME-13).
func TestMemberMarksADayAndTheTeamWeekShowsIt(t *testing.T) {
	e := newAPIEnv(t)
	e.register("day-mark@x.test")
	today := time.Now().UTC().Format("2006-01-02")

	saved := e.do("POST", "/api/day-marks", url.Values{"day": {today}, "kind": {"overload"}, "note": {"релиз"}}, nil)
	readBody(t, saved)
	if saved.StatusCode != http.StatusSeeOther || strings.Contains(saved.Header.Get("Location"), "bad_request") {
		t.Fatalf("mark the day: status %d to %q", saved.StatusCode, saved.Header.Get("Location"))
	}

	page := reactData[statsData](t, readBody(t, e.do("GET", "/stats", nil, nil)))
	if page.MarkWeek == nil || len(page.MarkWeek.Marks) != 1 || page.MarkWeek.Marks[0].Kind != "overload" || page.MarkWeek.Marks[0].Note != "релиз" {
		t.Fatalf("the viewer's marks are %+v, want the overload with its note", page.MarkWeek)
	}
	if page.TeamWeek == nil || len(page.TeamWeek.Rows) == 0 {
		t.Fatal("the manager's team week is missing")
	}
	found := false
	for _, row := range page.TeamWeek.Rows {
		for _, cell := range row.Cells {
			if cell.Mark != "" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("the team week does not show the day's mark")
	}

	bad := e.do("POST", "/api/day-marks", url.Values{"day": {today}, "kind": {"holiday"}}, nil)
	readBody(t, bad)
	if !strings.Contains(bad.Header.Get("Location"), "bad_request") {
		t.Fatalf("an unknown kind was accepted: %q", bad.Header.Get("Location"))
	}
}
