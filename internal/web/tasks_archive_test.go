package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"testing"
)

// An archived task keeps its card, read-only: it opens, edits are refused as
// archived rather than missing, the archive lists it, and putting it back on
// the board makes the card editable again.
func TestArchivedTaskCardIsReadOnlyAndComesBack(t *testing.T) {
	e := newAPIEnv(t)
	e.register("tasks-archive@x.test")
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Archive work"}}, nil))
	page := reactData[tasksPage](t, readBody(t, e.do("GET", "/tasks", nil, nil)))
	projectID := strconv.FormatInt(page.Projects[0].ID, 10)
	jsonHeader := map[string]string{"Accept": "application/json"}

	created := e.do("POST", "/api/tasks", url.Values{"project_id": {projectID}, "title": {"Old brief"}}, jsonHeader)
	body := readBody(t, created)
	var made taskCreatedResponse
	if err := json.Unmarshal([]byte(body), &made); err != nil || made.ID == 0 {
		t.Fatalf("create a task: no id in %s (%v)", body, err)
	}
	taskID := strconv.FormatInt(made.ID, 10)

	archived := e.do("POST", "/api/tasks/"+taskID+"/archive", nil, jsonHeader)
	readBody(t, archived)
	if archived.StatusCode != 200 {
		t.Fatalf("archive a task: status %d", archived.StatusCode)
	}
	if open := taskList(t, e, projectID); len(open) != 0 {
		t.Fatalf("an archived task is on the open board: %+v", open)
	}
	if list := taskListArchived(t, e, projectID); len(list) != 1 || !list[0].Archived || !list[0].CanArchive {
		t.Fatalf("the archive lists %+v, want the task, archived and restorable", list)
	}
	archivePage := reactData[tasksPage](t, readBody(t, e.do("GET", "/tasks?view=archive", nil, nil)))
	if archivePage.View != "archive" || len(archivePage.Tasks) != 1 {
		t.Fatalf("the archive page shows view %q with %d tasks, want archive with 1", archivePage.View, len(archivePage.Tasks))
	}

	card := e.do("GET", "/tasks/"+taskID, nil, nil)
	detail := reactData[taskDetailPage](t, readBody(t, card))
	if card.StatusCode != 200 || !detail.Task.Archived || len(detail.Linkable) != 0 {
		t.Fatalf("the archived card: status %d, archived %v, %d linkable; want 200, archived, none", card.StatusCode, detail.Task.Archived, len(detail.Linkable))
	}

	for name, call := range map[string]func() *http.Response{
		"title": func() *http.Response {
			return e.do("POST", "/api/tasks/"+taskID, url.Values{"title": {"Renamed"}}, jsonHeader)
		},
		"status": func() *http.Response {
			return e.do("POST", "/api/tasks/"+taskID+"/status", url.Values{"status": {"done"}}, jsonHeader)
		},
		"activity": func() *http.Response {
			return e.do("POST", "/api/tasks/"+taskID+"/activities", url.Values{"name": {"Late"}}, jsonHeader)
		},
	} {
		resp := call()
		readBody(t, resp)
		if resp.StatusCode != 409 {
			t.Fatalf("a %s change to an archived task: status %d, want 409", name, resp.StatusCode)
		}
	}
	linkable := e.do("GET", "/api/tasks/"+taskID+"/linkable", nil, jsonHeader)
	readBody(t, linkable)
	if linkable.StatusCode != 200 {
		t.Fatalf("linkable on an archived task: status %d, want 200", linkable.StatusCode)
	}

	restored := e.do("POST", "/api/tasks/"+taskID+"/restore", nil, jsonHeader)
	readBody(t, restored)
	if restored.StatusCode != 200 {
		t.Fatalf("restore a task: status %d", restored.StatusCode)
	}
	if open := taskList(t, e, projectID); len(open) != 1 || open[0].Archived {
		t.Fatalf("a restored task on the board: %+v", open)
	}
	if list := taskListArchived(t, e, projectID); len(list) != 0 {
		t.Fatalf("a restored task is still in the archive: %+v", list)
	}
	renamed := e.do("POST", "/api/tasks/"+taskID, url.Values{"title": {"Renamed"}}, jsonHeader)
	readBody(t, renamed)
	if renamed.StatusCode != 200 {
		t.Fatalf("an edit after the restore: status %d, want 200", renamed.StatusCode)
	}

	missing := e.do("POST", "/api/tasks/999999/restore", nil, jsonHeader)
	readBody(t, missing)
	if missing.StatusCode != 404 {
		t.Fatalf("restore a missing task: status %d, want 404", missing.StatusCode)
	}
}

func taskListArchived(t *testing.T, e *apiEnv, projectID string) []taskItem {
	t.Helper()
	resp := e.do("GET", "/api/tasks?project="+projectID+"&archived=1", nil, map[string]string{"Accept": "application/json"})
	body := readBody(t, resp)
	if resp.StatusCode != 200 {
		t.Fatalf("list archived tasks: status %d, body %s", resp.StatusCode, body)
	}
	var out tasksListResponse
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("list archived tasks: %v in %s", err, body)
	}
	return out.Tasks
}
