package web

import (
	"encoding/json"
	"net/url"
	"strconv"
	"testing"
	"time"
)

// The web side of archiving: archiving closes the timer that ran on the task,
// a new timer or a backfill on it is refused with a conflict or a bad request,
// and once the task is back on the board the timer starts again.
func TestArchivedTaskRefusesTimeUntilItIsBack(t *testing.T) {
	e := newAPIEnv(t)
	e.register("tasks-timer@x.test")
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Timer work"}}, nil))
	page := reactData[tasksPage](t, readBody(t, e.do("GET", "/tasks", nil, nil)))
	projectID := strconv.FormatInt(page.Projects[0].ID, 10)
	jsonHeader := map[string]string{"Accept": "application/json"}
	htmxHeader := map[string]string{"HX-Request": "true"}

	created := e.do("POST", "/api/tasks", url.Values{"project_id": {projectID}, "title": {"Timed brief"}}, jsonHeader)
	body := readBody(t, created)
	var made taskCreatedResponse
	if err := json.Unmarshal([]byte(body), &made); err != nil || made.ID == 0 {
		t.Fatalf("create a task: no id in %s (%v)", body, err)
	}
	taskID := strconv.FormatInt(made.ID, 10)

	started := e.do("POST", "/api/start", url.Values{"activity": {"Timed brief"}}, htmxHeader)
	readBody(t, started)
	if started.StatusCode >= 300 {
		t.Fatalf("start a timer on the task: status %d", started.StatusCode)
	}

	archived := e.do("POST", "/api/tasks/"+taskID+"/archive", nil, jsonHeader)
	readBody(t, archived)
	if archived.StatusCode != 200 {
		t.Fatalf("archive a task: status %d", archived.StatusCode)
	}
	var running int
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM sessions WHERE end_at IS NULL`).Scan(&running); err != nil {
		t.Fatalf("count running timers: %v", err)
	}
	if running != 0 {
		t.Fatalf("%d timers still run after the archive", running)
	}

	again := e.do("POST", "/api/start", url.Values{"activity": {"Timed brief"}}, htmxHeader)
	readBody(t, again)
	if again.StatusCode != 409 {
		t.Fatalf("a timer on an archived task: status %d, want 409", again.StatusCode)
	}
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	backfill := e.do("POST", "/api/sessions/backfill", url.Values{
		"activity": {"Timed brief"}, "start": {yesterday + " 09:00"}, "end": {yesterday + " 09:30"},
	}, jsonHeader)
	readBody(t, backfill)
	if backfill.Header.Get("X-Backfill-Saved") != "" || backfill.Header.Get("X-Backfill-Field") != "activity" {
		t.Fatalf("a backfill on an archived task was saved or not refused on the activity (field %q)", backfill.Header.Get("X-Backfill-Field"))
	}

	restored := e.do("POST", "/api/tasks/"+taskID+"/restore", nil, jsonHeader)
	readBody(t, restored)
	if restored.StatusCode != 200 {
		t.Fatalf("restore a task: status %d", restored.StatusCode)
	}
	resumed := e.do("POST", "/api/start", url.Values{"activity": {"Timed brief"}}, htmxHeader)
	readBody(t, resumed)
	if resumed.StatusCode >= 300 {
		t.Fatalf("a timer after the restore: status %d", resumed.StatusCode)
	}
}
