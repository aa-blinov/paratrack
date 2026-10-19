package web

import (
	"net/url"
	"strconv"
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
