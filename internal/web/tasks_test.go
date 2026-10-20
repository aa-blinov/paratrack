package web

import (
	"encoding/json"
	"net/url"
	"strconv"
	"testing"
)

// A task opens with an activity of its own name, takes more activities by
// name, moves between columns, and leaves the board when archived.
func TestTaskBoardHoldsActivitiesAndLeavesWhenArchived(t *testing.T) {
	e := newAPIEnv(t)
	e.register("tasks-web@x.test")
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Board work"}}, nil))
	page := reactData[tasksPage](t, readBody(t, e.do("GET", "/tasks", nil, nil)))
	if len(page.Projects) != 1 {
		t.Fatalf("tasks page has %d projects, want 1", len(page.Projects))
	}
	projectID := strconv.FormatInt(page.Projects[0].ID, 10)
	jsonHeader := map[string]string{"Accept": "application/json"}

	created := e.do("POST", "/api/tasks", url.Values{"project_id": {projectID}, "title": {"Write the brief"}}, jsonHeader)
	body := readBody(t, created)
	if created.StatusCode != 200 {
		t.Fatalf("create a task: status %d, body %s", created.StatusCode, body)
	}
	var made taskCreatedResponse
	if err := json.Unmarshal([]byte(body), &made); err != nil || made.ID == 0 {
		t.Fatalf("create a task: no id in %s (%v)", body, err)
	}
	taskID := strconv.FormatInt(made.ID, 10)

	list := taskList(t, e, projectID)
	if len(list) != 1 || list[0].Status != "todo" || len(list[0].Activities) != 1 || !list[0].CanArchive {
		t.Fatalf("a new task on the board: %+v", list)
	}

	added := e.do("POST", "/api/tasks/"+taskID+"/activities", url.Values{"name": {"Review"}}, jsonHeader)
	addedBody := readBody(t, added)
	if added.StatusCode != 200 {
		t.Fatalf("add an activity: status %d, body %s", added.StatusCode, addedBody)
	}
	if list = taskList(t, e, projectID); len(list[0].Activities) != 2 {
		t.Fatalf("the task holds %d activities after adding one, want 2", len(list[0].Activities))
	}

	moved := e.do("POST", "/api/tasks/"+taskID+"/status", url.Values{"status": {"doing"}}, jsonHeader)
	readBody(t, moved)
	if moved.StatusCode != 200 {
		t.Fatalf("move a task: status %d", moved.StatusCode)
	}
	if list = taskList(t, e, projectID); list[0].Status != "doing" {
		t.Fatalf("the task is in %q after a move to doing", list[0].Status)
	}

	refused := e.do("POST", "/api/tasks/"+taskID+"/status", url.Values{"status": {"someday"}}, jsonHeader)
	readBody(t, refused)
	if refused.StatusCode != 400 {
		t.Fatalf("an unknown status: status %d, want 400", refused.StatusCode)
	}

	detail := reactData[taskDetailPage](t, readBody(t, e.do("GET", "/tasks/"+taskID, nil, nil)))
	if detail.Task.Title != "Write the brief" || len(detail.Task.Activities) != 2 {
		t.Fatalf("the task card shows %q with %d activities", detail.Task.Title, len(detail.Task.Activities))
	}

	archived := e.do("POST", "/api/tasks/"+taskID+"/archive", nil, jsonHeader)
	readBody(t, archived)
	if archived.StatusCode != 200 {
		t.Fatalf("archive a task: status %d", archived.StatusCode)
	}
	if list = taskList(t, e, projectID); len(list) != 0 {
		t.Fatalf("an archived task is still on the board: %+v", list)
	}
}

func TestTaskCardOfAMissingTaskIsNotFound(t *testing.T) {
	e := newAPIEnv(t)
	e.register("tasks-missing@x.test")
	resp := e.do("GET", "/tasks/999999", nil, nil)
	readBody(t, resp)
	if resp.StatusCode != 404 {
		t.Fatalf("a task that does not exist: status %d, want 404", resp.StatusCode)
	}
}

func taskList(t *testing.T, e *apiEnv, projectID string) []taskItem {
	t.Helper()
	resp := e.do("GET", "/api/tasks?project="+projectID, nil, map[string]string{"Accept": "application/json"})
	body := readBody(t, resp)
	if resp.StatusCode != 200 {
		t.Fatalf("list tasks: status %d, body %s", resp.StatusCode, body)
	}
	var out tasksListResponse
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("list tasks: %v in %s", err, body)
	}
	return out.Tasks
}
