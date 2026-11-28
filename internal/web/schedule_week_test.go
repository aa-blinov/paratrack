package web

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Repeating last week needs an empty target: a week with a plan is refused,
// not merged, and an empty source has nothing to repeat.
func TestScheduleCopyRepeatsPreviousWeekOnlyIntoEmptyWeek(t *testing.T) {
	e := newAPIEnv(t)
	e.register("copy-week@x.test")
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Copy work"}}, nil))
	page := reactData[schedulePage](t, readBody(t, e.do("GET", "/schedule", nil, nil)))
	if len(page.People) != 1 || len(page.Projects) != 1 {
		t.Fatalf("schedule has %d people and %d projects, want 1 and 1", len(page.People), len(page.Projects))
	}
	userID := strconv.FormatInt(page.People[0].UserID, 10)
	projectID := strconv.FormatInt(page.Projects[0].ID, 10)

	thisMonday := startOfWeek(nil, time.Now())
	lastMonday := thisMonday.AddDate(0, 0, -7)
	// Nothing to repeat yet: last week is empty.
	empty := e.do("POST", "/api/schedule/copy", url.Values{"week": {thisMonday.Format("2006-01-02")}}, map[string]string{"Accept": "application/json"})
	empty.Body.Close()
	if empty.StatusCode != 409 {
		t.Fatalf("copy from an empty week: status %d, want 409", empty.StatusCode)
	}

	// Plan last week, then repeat it into this week.
	planned := e.do("POST", "/api/schedule/cell", url.Values{
		"user_id": {userID}, "project_id": {projectID}, "date": {lastMonday.AddDate(0, 0, 1).Format("2006-01-02")}, "minutes": {"240"},
	}, map[string]string{"Accept": "application/json"})
	planned.Body.Close()
	copied := e.do("POST", "/api/schedule/copy", url.Values{"week": {thisMonday.Format("2006-01-02")}}, map[string]string{"Accept": "application/json"})
	copied.Body.Close()
	if copied.StatusCode != 200 {
		t.Fatalf("copy into an empty week: status %d", copied.StatusCode)
	}
	after := reactData[schedulePage](t, readBody(t, e.do("GET", "/schedule?date="+thisMonday.Format("2006-01-02"), nil, nil)))
	if after.GrandMin != 240 || after.WeekEmpty {
		t.Fatalf("this week after the copy: %d minutes, empty=%v; want 240 and not empty", after.GrandMin, after.WeekEmpty)
	}

	// This week is no longer empty: a second copy is refused.
	again := e.do("POST", "/api/schedule/copy", url.Values{"week": {thisMonday.Format("2006-01-02")}}, map[string]string{"Accept": "application/json"})
	again.Body.Close()
	if again.StatusCode != 409 {
		t.Fatalf("copy into a planned week: status %d, want 409", again.StatusCode)
	}
}

// The admin time a manager sets comes off the available hours, and the load
// is then measured against what is left.
func TestScheduleAdminTimeComesOffAvailableHours(t *testing.T) {
	e := newAPIEnv(t)
	e.register("admin-time@x.test")
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Admin work"}}, nil))
	page := reactData[schedulePage](t, readBody(t, e.do("GET", "/schedule", nil, nil)))
	if len(page.People) != 1 {
		t.Fatalf("schedule has %d people, want 1", len(page.People))
	}
	userID := strconv.FormatInt(page.People[0].UserID, 10)
	if page.People[0].AvailableMin != 2400 {
		t.Fatalf("available before admin time = %d, want 2400 (480*5)", page.People[0].AvailableMin)
	}
	set := e.do("POST", "/api/schedule/admin", url.Values{
		"user_id": {userID}, "minutes": {"600"}, "week": {startOfWeek(nil, time.Now()).Format("2006-01-02")},
	}, map[string]string{"Accept": "application/json"})
	set.Body.Close()
	if set.StatusCode != 200 {
		t.Fatalf("set admin time: status %d", set.StatusCode)
	}
	after := reactData[schedulePage](t, readBody(t, e.do("GET", "/schedule", nil, nil)))
	if after.People[0].AdminMin != 600 || after.People[0].AvailableMin != 1800 {
		t.Fatalf("after admin time: admin=%d available=%d, want 600 and 1800", after.People[0].AdminMin, after.People[0].AvailableMin)
	}
}

// Repeating the week on screen forward fills the empty weeks ahead and
// skips, by name, a week that already has a plan.
func TestScheduleRepeatFillsEmptyWeeksAheadAndSkipsPlannedOnes(t *testing.T) {
	e := newAPIEnv(t)
	e.register("repeat-week@x.test")
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Repeat work"}}, nil))
	page := reactData[schedulePage](t, readBody(t, e.do("GET", "/schedule", nil, nil)))
	userID := strconv.FormatInt(page.People[0].UserID, 10)
	projectID := strconv.FormatInt(page.Projects[0].ID, 10)
	json := map[string]string{"Accept": "application/json"}
	monday := startOfWeek(nil, time.Now()).AddDate(0, 0, 14)
	cell := func(day time.Time, minutes string) {
		e.do("POST", "/api/schedule/cell", url.Values{"user_id": {userID}, "project_id": {projectID}, "date": {day.Format("2006-01-02")}, "minutes": {minutes}}, json).Body.Close()
	}
	cell(monday.AddDate(0, 0, 1), "240")
	// The second week ahead already has its own plan.
	cell(monday.AddDate(0, 0, 14+2), "60")
	resp := e.do("POST", "/api/schedule/copy", url.Values{"week": {monday.Format("2006-01-02")}, "weeks": {"3"}}, json)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("repeat: status %d", resp.StatusCode)
	}
	if toast, _ := url.PathUnescape(resp.Header.Get("X-Toast")); toast == "" || !strings.Contains(toast, monday.AddDate(0, 0, 14).Format("02.01")) {
		t.Fatalf("the skipped week is not named: %q", toast)
	}
	week := func(offset int) int {
		return reactData[schedulePage](t, readBody(t, e.do("GET", "/schedule?date="+monday.AddDate(0, 0, offset).Format("2006-01-02"), nil, nil))).GrandMin
	}
	if week(7) != 240 || week(14) != 60 || week(21) != 240 {
		t.Fatalf("weeks ahead: %d, %d, %d; want 240, 60 (kept), 240", week(7), week(14), week(21))
	}
	resp = e.do("POST", "/api/schedule/copy", url.Values{"week": {monday.Format("2006-01-02")}, "weeks": {"99"}}, json)
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("too many weeks: status %d, want 400", resp.StatusCode)
	}
}

// A repeated plan does not land on a day the person is away or on a holiday.
func TestScheduleRepeatSkipsAbsencesAndHolidays(t *testing.T) {
	e := newAPIEnv(t)
	e.register("repeat-away@x.test")
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Away work"}}, nil))
	page := reactData[schedulePage](t, readBody(t, e.do("GET", "/schedule", nil, nil)))
	userID := strconv.FormatInt(page.People[0].UserID, 10)
	projectID := strconv.FormatInt(page.Projects[0].ID, 10)
	json := map[string]string{"Accept": "application/json"}
	monday := startOfWeek(nil, time.Now()).AddDate(0, 0, 14)
	for d := 0; d < 3; d++ {
		e.do("POST", "/api/schedule/cell", url.Values{"user_id": {userID}, "project_id": {projectID}, "date": {monday.AddDate(0, 0, d).Format("2006-01-02")}, "minutes": {"60"}}, json).Body.Close()
	}
	next := monday.AddDate(0, 0, 7)
	e.do("POST", "/api/absences", url.Values{"user_id": {userID}, "kind": {"vacation"}, "from": {next.Format("2006-01-02")}, "to": {next.Format("2006-01-02")}}, nil).Body.Close()
	e.do("POST", "/api/holidays", url.Values{"from": {next.AddDate(0, 0, 1).Format("2006-01-02")}, "to": {next.AddDate(0, 0, 1).Format("2006-01-02")}, "name": {"Праздник"}}, nil).Body.Close()
	resp := e.do("POST", "/api/schedule/copy", url.Values{"week": {monday.Format("2006-01-02")}, "weeks": {"1"}}, json)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("repeat: %d", resp.StatusCode)
	}
	got := reactData[schedulePage](t, readBody(t, e.do("GET", "/schedule?date="+next.Format("2006-01-02"), nil, nil))).GrandMin
	if got != 60 {
		t.Fatalf("the repeated week holds %d minutes, want 60 (only the Wednesday)", got)
	}
}
