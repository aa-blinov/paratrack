package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// The task report answers for a period (TASK-04): the page lists the tasks with
// time on the days asked for, the CSV carries the same rows, and a period that ends
// before it starts is refused on both.
func TestTaskReportAnswersForAPeriod(t *testing.T) {
	e := newAPIEnv(t)
	e.register("task-report@x.test")
	jsonHeader := map[string]string{"Accept": "application/json"}

	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Report work"}}, nil))
	page := reactData[tasksPage](t, readBody(t, e.do("GET", "/tasks", nil, nil)))
	projectID := strconv.FormatInt(page.Projects[0].ID, 10)
	created := e.do("POST", "/api/tasks", url.Values{"project_id": {projectID}, "title": {"Report brief"}}, jsonHeader)
	readBody(t, created)
	if created.StatusCode != 200 {
		t.Fatalf("create the task: status %d", created.StatusCode)
	}

	// An hour on the task's own activity, on a fixed day.
	saved := e.do("POST", "/api/sessions/backfill", url.Values{
		"activity": {"Report brief"}, "start": {"20.10.2026 09:00"}, "end": {"20.10.2026 10:00"},
	}, nil)
	readBody(t, saved)
	if saved.Header.Get("X-Backfill-Saved") != "true" {
		t.Fatalf("the hour was not saved: status %d", saved.StatusCode)
	}

	report := reactData[tasksPage](t, readBody(t, e.do("GET", "/tasks?view=report&from=2026-10-20&to=2026-10-20", nil, nil)))
	if len(report.ReportRows) != 1 {
		t.Fatalf("the report holds %d rows for the day, want 1", len(report.ReportRows))
	}
	if row := report.ReportRows[0]; row.Title != "Report brief" || row.Seconds != 3600 {
		t.Fatalf("the report row is %q with %d s, want the task with 3600 s", row.Title, row.Seconds)
	}
	if report.ReportFrom != "2026-10-20" || report.ReportTo != "2026-10-20" || report.ReportNote != "" {
		t.Fatalf("the period is %q to %q with note %q", report.ReportFrom, report.ReportTo, report.ReportNote)
	}

	csvResponse := e.do("GET", "/api/tasks/report.csv?from=2026-10-20&to=2026-10-20", nil, nil)
	body := readBody(t, csvResponse)
	if csvResponse.StatusCode != http.StatusOK || !strings.HasPrefix(csvResponse.Header.Get("Content-Type"), "text/csv") {
		t.Fatalf("the CSV answered %d as %q", csvResponse.StatusCode, csvResponse.Header.Get("Content-Type"))
	}
	if !strings.HasPrefix(body, "task,project,assignee,status,estimate_hours,tracked_hours,tracked_seconds,due\n") {
		t.Fatalf("the CSV header is wrong: %q", body)
	}
	if !strings.Contains(body, "Report brief,Report work,") || !strings.Contains(body, ",1.00,3600,") {
		t.Fatalf("the CSV row is missing the hour: %q", body)
	}

	// A day with no time on tasks is an empty report, not an error.
	empty := reactData[tasksPage](t, readBody(t, e.do("GET", "/tasks?view=report&from=2026-10-21&to=2026-10-21", nil, nil)))
	if len(empty.ReportRows) != 0 || empty.ReportNote != "" {
		t.Fatalf("an empty day shows %d rows and note %q", len(empty.ReportRows), empty.ReportNote)
	}

	// A period that ends before it starts says so, and the CSV refuses it.
	inverted := reactData[tasksPage](t, readBody(t, e.do("GET", "/tasks?view=report&from=2026-10-21&to=2026-10-20", nil, nil)))
	if inverted.ReportNote == "" {
		t.Fatal("an inverted period shows no reason")
	}
	refused := e.do("GET", "/api/tasks/report.csv?from=2026-10-21&to=2026-10-20", nil, nil)
	readBody(t, refused)
	if refused.StatusCode != http.StatusBadRequest {
		t.Fatalf("an inverted CSV period answered %d, want 400", refused.StatusCode)
	}
}
