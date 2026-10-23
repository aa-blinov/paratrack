package web

import (
	"net/url"
	"strconv"
	"testing"
)

// A task being created and a task reaching done are webhook events, written in the same
// transaction as the change (ADM-14).
func TestTaskCreatedAndDoneAreWebhookEvents(t *testing.T) {
	e := newAPIEnv(t)
	e.register("task-events@x.test")
	jsonHeader := map[string]string{"Accept": "application/json"}

	// An event is written only when an endpoint subscribes to it.
	readBody(t, e.do("POST", "/api/webhooks", url.Values{"url": {"https://example.com/tasks"}, "secret": {"task-secret"}, "events": {"task.created,task.done"}}, nil))
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Events work"}}, nil))
	page := reactData[tasksPage](t, readBody(t, e.do("GET", "/tasks", nil, nil)))
	projectID := strconv.FormatInt(page.Projects[len(page.Projects)-1].ID, 10)
	readBody(t, e.do("POST", "/api/tasks", url.Values{"project_id": {projectID}, "title": {"Ship events"}}, jsonHeader))
	var taskID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM tasks WHERE title = 'Ship events'`).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	countEvents := func(name string) int {
		var n int
		if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT count(*) FROM webhook_event_outbox WHERE event = ?`, name).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := countEvents("task.created"); n != 1 {
		t.Fatalf("task.created events after creating the task: %d, want 1", n)
	}
	if n := countEvents("task.done"); n != 0 {
		t.Fatalf("task.done before the task is done: %d", n)
	}

	readBody(t, e.do("POST", "/api/tasks/"+strconv.FormatInt(taskID, 10)+"/status", url.Values{"status": {"done"}}, jsonHeader))
	if n := countEvents("task.done"); n != 1 {
		t.Fatalf("task.done events after the task is done: %d, want 1", n)
	}
}
