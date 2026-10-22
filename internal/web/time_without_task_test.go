package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// Time logged to an activity that is on no task is kept, and the save says so
// (TIME-05). Time on an activity that a task holds says nothing extra.
func TestTimeWithoutTaskIsNamedOnSave(t *testing.T) {
	e := newAPIEnv(t)
	e.register("no-task@x.test")
	jsonHeader := map[string]string{"Accept": "application/json"}

	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Tasked work"}}, nil))
	page := reactData[tasksPage](t, readBody(t, e.do("GET", "/tasks", nil, nil)))
	projectID := strconv.FormatInt(page.Projects[0].ID, 10)

	created := e.do("POST", "/api/tasks", url.Values{"project_id": {projectID}, "title": {"Held brief"}}, jsonHeader)
	readBody(t, created)
	if created.StatusCode != 200 {
		t.Fatalf("create the task: status %d", created.StatusCode)
	}

	// Stop a timer on an activity with no task: the toast names the gap.
	started := e.do("POST", "/api/start", url.Values{"activity": {"Loose thinking"}}, jsonHeader)
	readBody(t, started)
	ids := activeSessionIDs(t, e)
	if len(ids) == 0 {
		t.Fatal("no running timer after start")
	}
	stopped := e.do("POST", "/api/sessions/"+strconv.FormatInt(ids[0], 10)+"/stop", nil, nil)
	stopped.Body.Close()
	if note := toastOf(t, stopped); !strings.Contains(note, "Без задачи") {
		t.Fatalf("stop on a task-less activity does not name it: %q", note)
	}

	// Stop a timer on the task's own activity: no note.
	started = e.do("POST", "/api/start", url.Values{"activity": {"Held brief"}}, jsonHeader)
	readBody(t, started)
	ids = activeSessionIDs(t, e)
	stopped = e.do("POST", "/api/sessions/"+strconv.FormatInt(ids[0], 10)+"/stop", nil, nil)
	stopped.Body.Close()
	if note := toastOf(t, stopped); strings.Contains(note, "Без задачи") {
		t.Fatalf("stop on a task's activity names it as taskless: %q", note)
	}

	// A manual entry on a task-less activity says so as well.
	saved := e.do("POST", "/api/sessions/backfill", url.Values{
		"activity": {"Loose manual"}, "start": {"yesterday 09:00"}, "end": {"yesterday 10:00"},
	}, nil)
	readBody(t, saved)
	if saved.Header.Get("X-Backfill-Saved") != "true" {
		t.Fatalf("manual entry was not saved: status %d", saved.StatusCode)
	}
	if note := toastOf(t, saved); !strings.Contains(note, "Без задачи") {
		t.Fatalf("manual entry on a task-less activity does not name it: %q", note)
	}
}

// toastOf reads the toast header a response carries, decoded as the page shows it.
func toastOf(t *testing.T, resp *http.Response) string {
	t.Helper()
	decoded, err := url.PathUnescape(resp.Header.Get("X-Toast"))
	if err != nil {
		t.Fatalf("decode the toast: %v", err)
	}
	return decoded
}
