package web

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/db"
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

// A start replayed from the offline queue is dated by the click, not by the reconnect.
func TestQueuedWorkdayStartKeepsTheClickTime(t *testing.T) {
	e := newAPIEnv(t)
	e.register("workday-queued@x.test")

	clicked := time.Now().Add(-2 * time.Hour).Truncate(time.Millisecond)
	response := e.do("POST", "/api/workday", map[string][]string{
		"action": {"start"}, "client_ts": {strconv.FormatInt(clicked.UnixMilli(), 10)},
	}, nil)
	readBody(t, response)
	var raw string
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT started_at FROM workday_marks ORDER BY day DESC LIMIT 1`).Scan(&raw); err != nil {
		t.Fatalf("read the start: %v", err)
	}
	started, err := db.ParseTime(raw)
	if err != nil {
		t.Fatalf("parse the start: %v", err)
	}
	if diff := started.Sub(clicked); diff < -time.Second || diff > time.Second {
		t.Fatalf("queued start recorded at %v, want the click at %v", started, clicked)
	}
}
