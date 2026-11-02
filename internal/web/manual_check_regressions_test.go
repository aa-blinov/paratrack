package web

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// Stopping a closed session inside an approved week is refused with the same
// 409 and message as an edit. It used to fall through to a 500.
func TestStopInsideApprovedWeekIsRefusedNotAServerError(t *testing.T) {
	e := newAPIEnv(t)
	e.register("stop-approved@x.test")
	readBody(t, e.do("POST", "/api/team/calendar", url.Values{"timezone": {"UTC"}, "week_start": {"mon"}}, nil))
	readBody(t, e.do("POST", "/api/sessions/backfill", url.Values{"activity": {"Согласование"}, "start": {"2020-03-04T10:00"}, "end": {"2020-03-04T11:00"}, "note": {"stop-check"}}, nil))
	readBody(t, e.do("POST", "/api/timesheet/approve", url.Values{"start": {"2020-03-02"}, "end": {"2020-03-08"}}, nil))

	var id int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM sessions WHERE note = 'stop-check'`).Scan(&id); err != nil {
		t.Fatalf("find the backfilled session: %v", err)
	}
	resp := e.do("POST", "/api/sessions/"+strconv.FormatInt(id, 10)+"/stop", nil, nil)
	body := readBody(t, resp)
	if resp.StatusCode != 409 {
		t.Fatalf("stop in an approved week = %d %q, want 409", resp.StatusCode, body)
	}
	if !strings.Contains(body, "утверждена") {
		t.Fatalf("stop refusal %q does not name the approved week", body)
	}
}

// An invoice period is read in the workspace zone. A week that starts at
// midnight Moscow is 19 October in Moscow, and the list must not print 18 October.
func TestInvoiceListPrintsThePeriodInTheWorkspaceZone(t *testing.T) {
	e := newAPIEnv(t)
	e.register("period-zone@x.test")
	readBody(t, e.do("POST", "/api/team/calendar", url.Values{"timezone": {"Europe/Moscow"}, "week_start": {"mon"}}, nil))
	readBody(t, e.do("POST", "/api/team/modules", url.Values{"preset": {"studio"}}, nil))
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Zone Check"}}, nil))
	readBody(t, e.do("POST", "/projects/zone-check/rate", url.Values{"slug": {"zone-check"}, "rate": {"5000"}, "billable": {"1"}}, nil))
	readBody(t, e.do("POST", "/api/sessions/backfill", url.Values{"activity": {"Зона"}, "start": {"2020-03-04T10:00"}, "end": {"2020-03-04T11:00"}, "note": {"zone-check"}}, nil))

	var activityID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT activity_id FROM sessions WHERE note = 'zone-check'`).Scan(&activityID); err != nil {
		t.Fatalf("find the activity: %v", err)
	}
	var projectID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM projects WHERE slug = 'zone-check'`).Scan(&projectID); err != nil {
		t.Fatalf("find the project: %v", err)
	}
	readBody(t, e.do("POST", "/api/activities/"+strconv.FormatInt(activityID, 10)+"/project", url.Values{"project_id": {strconv.FormatInt(projectID, 10)}}, nil))
	// An invoice takes only approved time, so the week is approved first, as in the app.
	readBody(t, e.do("POST", "/api/timesheet/approve", url.Values{"start": {"2020-03-02"}, "end": {"2020-03-08"}}, nil))
	readBody(t, e.do("POST", "/invoices", url.Values{"client": {"Zone Client"}, "start": {"2020-03-02"}, "end": {"2020-03-08"}, "project_id": {strconv.FormatInt(projectID, 10)}}, nil))

	list := readBody(t, e.do("GET", "/invoices", nil, nil))
	if !strings.Contains(list, "2 мар – 8 мар") {
		t.Fatalf("invoice list does not print the workspace-zone period 2 мар – 8 мар")
	}
}
