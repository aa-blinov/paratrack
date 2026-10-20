package web

import (
	"net/url"
	"strconv"
	"testing"
	"time"
)

// A project put on a person's schedule stays there with no hours, is shown
// after a reload, and is taken off only when it carries no hours.
func TestScheduleLineStaysUntilItHasNoHoursAndComesOff(t *testing.T) {
	e := newAPIEnv(t)
	e.register("lines-web@x.test")
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Line work"}}, nil))
	page := reactData[schedulePage](t, readBody(t, e.do("GET", "/schedule", nil, nil)))
	if len(page.People) != 1 || len(page.Projects) != 1 {
		t.Fatalf("schedule has %d people and %d projects, want 1 and 1", len(page.People), len(page.Projects))
	}
	userID := strconv.FormatInt(page.People[0].UserID, 10)
	projectID := strconv.FormatInt(page.Projects[0].ID, 10)
	week := startOfWeek(nil, time.Now()).Format("2006-01-02")
	form := url.Values{"user_id": {userID}, "project_id": {projectID}, "week": {week}}
	json := map[string]string{"Accept": "application/json"}

	added := e.do("POST", "/api/schedule/line", form, json)
	added.Body.Close()
	if added.StatusCode != 200 {
		t.Fatalf("add a line: status %d", added.StatusCode)
	}
	after := reactData[schedulePage](t, readBody(t, e.do("GET", "/schedule", nil, nil)))
	if len(after.People[0].Lines) != 1 || after.People[0].Lines[0].ProjectID != page.Projects[0].ID || after.People[0].Lines[0].TotalMin != 0 {
		t.Fatalf("the line did not survive a reload with no hours: %+v", after.People[0].Lines)
	}

	planned := e.do("POST", "/api/schedule/cell", url.Values{
		"user_id": {userID}, "project_id": {projectID}, "date": {week}, "minutes": {"60"},
	}, json)
	planned.Body.Close()
	refused := e.do("POST", "/api/schedule/line/remove", form, json)
	refused.Body.Close()
	if refused.StatusCode != 409 {
		t.Fatalf("remove a line with hours: status %d, want 409", refused.StatusCode)
	}

	cleared := e.do("POST", "/api/schedule/cell", url.Values{
		"user_id": {userID}, "project_id": {projectID}, "date": {week}, "minutes": {"0"},
	}, json)
	cleared.Body.Close()
	removed := e.do("POST", "/api/schedule/line/remove", form, json)
	removed.Body.Close()
	if removed.StatusCode != 200 {
		t.Fatalf("remove an empty line: status %d", removed.StatusCode)
	}
	gone := reactData[schedulePage](t, readBody(t, e.do("GET", "/schedule", nil, nil)))
	if len(gone.People[0].Lines) != 0 {
		t.Fatalf("a removed line is still on the schedule: %+v", gone.People[0].Lines)
	}
}
