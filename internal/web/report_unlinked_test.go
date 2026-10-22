package web

import (
	"net/url"
	"strconv"
	"testing"
)

// The report says how much of the period's time sits on no task (TIME-05): the
// manager sees the gap, not only the totals.
func TestReportNamesTheShareOfTimeWithoutTask(t *testing.T) {
	e := newAPIEnv(t)
	e.register("report-unlinked@x.test")
	jsonHeader := map[string]string{"Accept": "application/json"}

	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Reported work"}}, nil))
	page := reactData[tasksPage](t, readBody(t, e.do("GET", "/tasks", nil, nil)))
	projectID := strconv.FormatInt(page.Projects[0].ID, 10)
	readBody(t, e.do("POST", "/api/tasks", url.Values{"project_id": {projectID}, "title": {"Held report brief"}}, jsonHeader))

	// One hour on the task's activity, two hours on an activity no task holds.
	for _, entry := range []struct{ activity, start, end string }{
		{"Held report brief", "yesterday 09:00", "yesterday 10:00"},
		{"Loose report time", "yesterday 11:00", "yesterday 13:00"},
	} {
		resp := e.do("POST", "/api/sessions/backfill", url.Values{
			"activity": {entry.activity}, "start": {entry.start}, "end": {entry.end}, "confirm_long": {"1"},
		}, nil)
		readBody(t, resp)
		if resp.Header.Get("X-Backfill-Saved") != "true" {
			t.Fatalf("backfill %q: status %d", entry.activity, resp.StatusCode)
		}
	}

	report := reactData[reportRunPage](t, readBody(t, e.do("GET", "/reports/run?id=by-day", nil, nil)))
	if !report.VM.ShowUnlinked {
		t.Fatalf("the report does not name the time without a task: %+v", report.VM)
	}
	if report.VM.UnlinkedPercent != "66%" {
		t.Fatalf("share without a task = %q, want 66%% (2 h of 3 h)", report.VM.UnlinkedPercent)
	}
}
