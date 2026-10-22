package web

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// Stopping a timer on a task's activity names the task, so the stop card can take
// the one-line result (TASK-02). A stop on a task-less activity names nothing, and
// the result line lands in the task's feed.
func TestStopOnTaskOffersTheResultLine(t *testing.T) {
	e := newAPIEnv(t)
	e.register("stop-result@x.test")
	jsonHeader := map[string]string{"Accept": "application/json"}

	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Result work"}}, nil))
	page := reactData[tasksPage](t, readBody(t, e.do("GET", "/tasks", nil, nil)))
	projectID := strconv.FormatInt(page.Projects[0].ID, 10)

	created := e.do("POST", "/api/tasks", url.Values{"project_id": {projectID}, "title": {"Result brief"}}, jsonHeader)
	readBody(t, created)
	if created.StatusCode != 200 {
		t.Fatalf("create the task: status %d", created.StatusCode)
	}

	readBody(t, e.do("POST", "/api/start", url.Values{"activity": {"Loose result"}}, jsonHeader))
	ids := activeSessionIDs(t, e)
	stopped := e.do("POST", "/api/sessions/"+strconv.FormatInt(ids[0], 10)+"/stop", nil, nil)
	stopped.Body.Close()
	if got := stopped.Header.Get("X-Toast-Task"); got != "" {
		t.Fatalf("a stop on a task-less activity names a task: %q", got)
	}

	readBody(t, e.do("POST", "/api/start", url.Values{"activity": {"Result brief"}}, jsonHeader))
	ids = activeSessionIDs(t, e)
	stopped = e.do("POST", "/api/sessions/"+strconv.FormatInt(ids[0], 10)+"/stop", nil, nil)
	stopped.Body.Close()
	taskID := stopped.Header.Get("X-Toast-Task")
	if taskID == "" || taskID == "0" {
		t.Fatalf("a stop on the task's activity does not name the task: %q", taskID)
	}

	comment := e.do("POST", "/api/tasks/"+taskID+"/comments", url.Values{"body": {"Brief written"}}, jsonHeader)
	body := readBody(t, comment)
	if comment.StatusCode != 200 || !strings.Contains(body, "Brief written") {
		t.Fatalf("the result line was not saved to the task: status %d, body %s", comment.StatusCode, body)
	}
}
