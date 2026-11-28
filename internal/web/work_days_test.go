package web

import (
	"net/url"
	"strconv"
	"testing"
	"time"
)

// A part-timer's week sets what the schedule expects of them: Monday to
// Wednesday is three days of capacity, not five. The production calendar
// fills the year's weekdays off, named.
func TestWorkDaysShapeCapacityAndCalendarFillsHolidays(t *testing.T) {
	e := newAPIEnv(t)
	e.register("part-time@x.test")
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Part work"}}, nil))
	page := reactData[schedulePage](t, readBody(t, e.do("GET", "/schedule", nil, nil)))
	userID := strconv.FormatInt(page.People[0].UserID, 10)
	full := page.People[0].AvailableMin
	e.do("POST", "/api/member/pay", url.Values{"user_id": {userID}, "capacity_minutes": {"480"}, "work_days_sent": {"1"}, "work_day": {"1", "2", "3"}}, nil).Body.Close()
	// A week with no holiday: the one after the next.
	week := startOfWeek(nil, time.Now()).AddDate(0, 0, 14).Format("2006-01-02")
	after := reactData[schedulePage](t, readBody(t, e.do("GET", "/schedule?date="+week, nil, nil)))
	if got := after.People[0].AvailableMin; got != 3*480 || full == got {
		t.Fatalf("a Monday-to-Wednesday week carries %d minutes (five days were %d), want %d", got, full, 3*480)
	}
	e.do("POST", "/api/member/pay", url.Values{"user_id": {userID}, "work_days_sent": {"1"}}, nil).Body.Close()
	var days string
	e.db.TestSQL().QueryRowContext(t.Context(), `SELECT work_days FROM memberships WHERE user_id = ?`, userID).Scan(&days)
	if days != "123" {
		t.Fatalf("unticking every day was saved: %q", days)
	}

	// A day the manager marked by hand keeps its mark.
	e.do("POST", "/api/holidays", url.Values{"from": {"2026-12-31"}, "to": {"2026-12-31"}, "name": {"работаем"}, "work": {"1"}}, nil).Body.Close()
	e.do("POST", "/api/holidays/preset", url.Values{"year": {"2026"}}, nil).Body.Close()
	var kept string
	e.db.TestSQL().QueryRowContext(t.Context(), `SELECT kind FROM team_holidays WHERE day = '2026-12-31'`).Scan(&kept)
	if kept != "work" {
		t.Fatalf("the calendar overwrote a hand-set day: %q", kept)
	}
	var count int
	var name string
	e.db.TestSQL().QueryRowContext(t.Context(), `SELECT COUNT(*), MAX(CASE WHEN day = '2026-06-12' THEN name END) FROM team_holidays WHERE kind = 'off'`).Scan(&count, &name)
	if count != 13 || name != "День России" {
		t.Fatalf("the 2026 calendar filled %d days, 12 June named %q", count, name)
	}
}
