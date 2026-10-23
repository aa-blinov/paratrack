package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// A task's checklist keeps steps in the order they were added, marks them done or not again,
// and removes them; the task page shows them (TASK-10).
func TestTaskChecklistAddsTogglesAndRemovesSteps(t *testing.T) {
	e := newAPIEnv(t)
	e.register("checklist@x.test")
	jsonHeader := map[string]string{"Accept": "application/json"}

	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Checklist work"}}, nil))
	page := reactData[tasksPage](t, readBody(t, e.do("GET", "/tasks", nil, nil)))
	projectID := strconv.FormatInt(page.Projects[len(page.Projects)-1].ID, 10)
	readBody(t, e.do("POST", "/api/tasks", url.Values{"project_id": {projectID}, "title": {"Ship the form"}}, jsonHeader))
	var taskID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM tasks WHERE title = 'Ship the form'`).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	base := "/api/tasks/" + strconv.FormatInt(taskID, 10) + "/checklist"

	first := e.do("POST", base, url.Values{"body": {"Написать тексты"}}, jsonHeader)
	readBody(t, first)
	second := e.do("POST", base, url.Values{"body": {"Проверить на телефоне"}}, jsonHeader)
	readBody(t, second)
	if first.StatusCode != http.StatusOK || second.StatusCode != http.StatusOK {
		t.Fatalf("add steps: %d, %d", first.StatusCode, second.StatusCode)
	}
	var firstID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM task_checklist_items WHERE body = 'Написать тексты'`).Scan(&firstID); err != nil {
		t.Fatal(err)
	}

	done := e.do("POST", base+"/"+strconv.FormatInt(firstID, 10)+"/done", url.Values{"done": {"1"}}, jsonHeader)
	readBody(t, done)
	if done.StatusCode != http.StatusOK {
		t.Fatalf("mark the step done: status %d", done.StatusCode)
	}
	detail := reactData[taskDetailPage](t, readBody(t, e.do("GET", "/tasks/"+strconv.FormatInt(taskID, 10), nil, nil)))
	if len(detail.Checklist) != 2 || detail.Checklist[0].Body != "Написать тексты" || !detail.Checklist[0].Done || detail.Checklist[1].Done {
		t.Fatalf("the task page shows %+v, want two steps in order, the first done", detail.Checklist)
	}

	removed := e.do("POST", base+"/"+strconv.FormatInt(firstID, 10)+"/delete", nil, jsonHeader)
	readBody(t, removed)
	if removed.StatusCode != http.StatusOK {
		t.Fatalf("remove the step: status %d", removed.StatusCode)
	}
	detail = reactData[taskDetailPage](t, readBody(t, e.do("GET", "/tasks/"+strconv.FormatInt(taskID, 10), nil, nil)))
	if len(detail.Checklist) != 1 || strings.Contains(detail.Checklist[0].Body, "тексты") {
		t.Fatalf("after the removal the page shows %+v", detail.Checklist)
	}
}
