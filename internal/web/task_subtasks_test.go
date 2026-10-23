package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// A subtask hangs under a top-level task of the same project. The parent's page lists the
// subtasks, and the rollup counts the done ones; a subtask of a subtask is refused (TASK-09).
func TestSubtasksRollUpUnderTheirParent(t *testing.T) {
	e := newAPIEnv(t)
	e.register("subtasks@x.test")
	jsonHeader := map[string]string{"Accept": "application/json"}

	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Subtask work"}}, nil))
	page := reactData[tasksPage](t, readBody(t, e.do("GET", "/tasks", nil, nil)))
	projectID := strconv.FormatInt(page.Projects[len(page.Projects)-1].ID, 10)

	readBody(t, e.do("POST", "/api/tasks", url.Values{"project_id": {projectID}, "title": {"Parent"}}, jsonHeader))
	var parentID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM tasks WHERE title = 'Parent'`).Scan(&parentID); err != nil {
		t.Fatal(err)
	}
	parent := strconv.FormatInt(parentID, 10)

	first := e.do("POST", "/api/tasks", url.Values{"project_id": {projectID}, "parent_id": {parent}, "title": {"Макет"}, "status": {"done"}}, jsonHeader)
	readBody(t, first)
	second := e.do("POST", "/api/tasks", url.Values{"project_id": {projectID}, "parent_id": {parent}, "title": {"Вёрстка"}}, jsonHeader)
	readBody(t, second)
	if first.StatusCode != http.StatusOK || second.StatusCode != http.StatusOK {
		t.Fatalf("create the subtasks: %d, %d", first.StatusCode, second.StatusCode)
	}

	detail := reactData[taskDetailPage](t, readBody(t, e.do("GET", "/tasks/"+parent, nil, nil)))
	if len(detail.Subtasks) != 2 || detail.Subtasks[0].Title != "Макет" || !detail.Subtasks[0].Done || detail.Subtasks[1].Done {
		t.Fatalf("the parent lists %+v, want two subtasks in order, the first done", detail.Subtasks)
	}
	if detail.SubtaskRollup == "" || !strings.Contains(detail.SubtaskRollup, "1") || !strings.Contains(detail.SubtaskRollup, "2") {
		t.Fatalf("the rollup line %q does not count one done of two", detail.SubtaskRollup)
	}

	var subID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM tasks WHERE title = 'Макет'`).Scan(&subID); err != nil {
		t.Fatal(err)
	}
	nested := e.do("POST", "/api/tasks", url.Values{"project_id": {projectID}, "parent_id": {strconv.FormatInt(subID, 10)}, "title": {"Глубже"}}, jsonHeader)
	readBody(t, nested)
	if nested.StatusCode == http.StatusOK {
		t.Fatal("a subtask of a subtask was created")
	}
	var deeper int
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT count(*) FROM tasks WHERE title = 'Глубже'`).Scan(&deeper); err != nil {
		t.Fatal(err)
	}
	if deeper != 0 {
		t.Fatalf("%d rows for the refused subtask", deeper)
	}
}
