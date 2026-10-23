package web

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"
)

// A task can wait for another in its project. A wait that would loop, or a self-wait, is refused.
// The waiting task is blocked until the other one is done, and a wait can be removed (TASK-11).
func TestTaskDependenciesBlockUntilTheWaitIsDone(t *testing.T) {
	e := newAPIEnv(t)
	e.register("dependencies@x.test")
	jsonHeader := map[string]string{"Accept": "application/json"}

	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Waits work"}}, nil))
	page := reactData[tasksPage](t, readBody(t, e.do("GET", "/tasks", nil, nil)))
	projectID := strconv.FormatInt(page.Projects[len(page.Projects)-1].ID, 10)
	for _, title := range []string{"Сборка", "Проверка"} {
		readBody(t, e.do("POST", "/api/tasks", url.Values{"project_id": {projectID}, "title": {title}}, jsonHeader))
	}
	var build, check int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM tasks WHERE title = 'Сборка'`).Scan(&build); err != nil {
		t.Fatal(err)
	}
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM tasks WHERE title = 'Проверка'`).Scan(&check); err != nil {
		t.Fatal(err)
	}
	buildID, checkID := strconv.FormatInt(build, 10), strconv.FormatInt(check, 10)

	waits := e.do("POST", "/api/tasks/"+buildID+"/dependencies", url.Values{"depends_on": {checkID}}, jsonHeader)
	readBody(t, waits)
	if waits.StatusCode != http.StatusOK {
		t.Fatalf("the build waits for the check: status %d", waits.StatusCode)
	}
	loop := e.do("POST", "/api/tasks/"+checkID+"/dependencies", url.Values{"depends_on": {buildID}}, jsonHeader)
	readBody(t, loop)
	if loop.StatusCode == http.StatusOK {
		t.Fatal("a wait that loops back was accepted")
	}
	self := e.do("POST", "/api/tasks/"+buildID+"/dependencies", url.Values{"depends_on": {buildID}}, jsonHeader)
	readBody(t, self)
	if self.StatusCode == http.StatusOK {
		t.Fatal("a task waiting for itself was accepted")
	}

	detail := reactData[taskDetailPage](t, readBody(t, e.do("GET", "/tasks/"+buildID, nil, nil)))
	if len(detail.WaitsFor) != 1 || detail.WaitsFor[0].ID != check || !detail.Blocked {
		t.Fatalf("the build shows waits %+v blocked %v, want the check and blocked", detail.WaitsFor, detail.Blocked)
	}
	checkDetail := reactData[taskDetailPage](t, readBody(t, e.do("GET", "/tasks/"+checkID, nil, nil)))
	if len(checkDetail.Blocks) != 1 || checkDetail.Blocks[0].ID != build {
		t.Fatalf("the check blocks %+v, want the build", checkDetail.Blocks)
	}

	readBody(t, e.do("POST", "/api/tasks/"+checkID+"/status", url.Values{"status": {"done"}}, jsonHeader))
	detail = reactData[taskDetailPage](t, readBody(t, e.do("GET", "/tasks/"+buildID, nil, nil)))
	if detail.Blocked {
		t.Fatal("the build is still blocked after its wait is done")
	}

	removed := e.do("POST", "/api/tasks/"+buildID+"/dependencies/"+checkID+"/delete", nil, jsonHeader)
	readBody(t, removed)
	if removed.StatusCode != http.StatusOK {
		t.Fatalf("remove the wait: status %d", removed.StatusCode)
	}
	detail = reactData[taskDetailPage](t, readBody(t, e.do("GET", "/tasks/"+buildID, nil, nil)))
	if len(detail.WaitsFor) != 0 {
		t.Fatalf("after the removal the build still waits for %+v", detail.WaitsFor)
	}
}
