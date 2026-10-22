package web

import (
	"net/http"
	"strings"
	"testing"
)

// The viewer starts and ends their day from the dashboard; the day's times come back
// on the page (TIME-06).
func TestDashboardStartsAndEndsTheWorkday(t *testing.T) {
	e := newAPIEnv(t)
	e.register("workday@x.test")

	started := e.do("POST", "/api/workday", map[string][]string{"action": {"start"}}, nil)
	readBody(t, started)
	if started.StatusCode != http.StatusSeeOther {
		t.Fatalf("start the day: status %d", started.StatusCode)
	}
	page := reactData[dashboardData](t, readBody(t, e.do("GET", "/", nil, nil)))
	if page.Workday.Started == "" || page.Workday.Ended != "" {
		t.Fatalf("after the start the dashboard shows %+v", page.Workday)
	}

	ended := e.do("POST", "/api/workday", map[string][]string{"action": {"end"}}, nil)
	readBody(t, ended)
	page = reactData[dashboardData](t, readBody(t, e.do("GET", "/", nil, nil)))
	if page.Workday.Ended == "" || !strings.Contains(page.Workday.Started, ":") {
		t.Fatalf("after the end the dashboard shows %+v", page.Workday)
	}
}
