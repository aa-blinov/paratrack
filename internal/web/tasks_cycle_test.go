package web

import (
	"encoding/json"
	"net/url"
	"strconv"
	"testing"
	"time"
)

// The whole task life, through the same endpoints the board uses: a task with
// an estimate takes a second activity, time logged to either activity counts
// on the task, and archiving takes it off the board without losing that time.
func TestTaskCycleCountsTimeLoggedToItsActivities(t *testing.T) {
	e := newAPIEnv(t)
	e.register("tasks-cycle@x.test")
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Cycle work"}}, nil))
	page := reactData[tasksPage](t, readBody(t, e.do("GET", "/tasks", nil, nil)))
	if len(page.Projects) != 1 {
		t.Fatalf("tasks page has %d projects, want 1", len(page.Projects))
	}
	projectID := strconv.FormatInt(page.Projects[0].ID, 10)
	jsonHeader := map[string]string{"Accept": "application/json"}

	created := e.do("POST", "/api/tasks", url.Values{
		"project_id": {projectID}, "title": {"Write the brief"}, "estimate_minutes": {"120"},
	}, jsonHeader)
	body := readBody(t, created)
	if created.StatusCode != 200 {
		t.Fatalf("create a task: status %d, body %s", created.StatusCode, body)
	}
	var made taskCreatedResponse
	if err := json.Unmarshal([]byte(body), &made); err != nil || made.ID == 0 {
		t.Fatalf("create a task: no id in %s (%v)", body, err)
	}
	taskID := strconv.FormatInt(made.ID, 10)

	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	logTime := func(activity string, from, to string) {
		t.Helper()
		resp := e.do("POST", "/api/sessions/backfill", url.Values{
			"activity": {activity}, "start": {yesterday + " " + from}, "end": {yesterday + " " + to},
		}, jsonHeader)
		readBody(t, resp)
		if resp.StatusCode >= 300 {
			t.Fatalf("log time on %q: status %d", activity, resp.StatusCode)
		}
	}

	logTime("Write the brief", "09:00", "09:30")
	list := taskList(t, e, projectID)
	if len(list) != 1 || list[0].TrackedMin != 30 || list[0].TrackedLabel == "" {
		t.Fatalf("30 minutes on the task's own activity show as %+v", list)
	}

	added := e.do("POST", "/api/tasks/"+taskID+"/activities", url.Values{"name": {"Review"}}, jsonHeader)
	readBody(t, added)
	if added.StatusCode != 200 {
		t.Fatalf("add an activity: status %d", added.StatusCode)
	}
	logTime("Review", "10:00", "10:15")
	list = taskList(t, e, projectID)
	if list[0].TrackedMin != 45 || len(list[0].Activities) != 2 {
		t.Fatalf("time on two activities shows %d minutes across %d activities, want 45 and 2", list[0].TrackedMin, len(list[0].Activities))
	}
	if list[0].EstimateMin != 120 {
		t.Fatalf("the estimate is %d minutes, want 120", list[0].EstimateMin)
	}

	detail := reactData[taskDetailPage](t, readBody(t, e.do("GET", "/tasks/"+taskID, nil, nil)))
	if detail.Task.TrackedMin != 45 || len(detail.Linkable) != 0 {
		t.Fatalf("the card shows %d minutes with %d linkable activities, want 45 and 0", detail.Task.TrackedMin, len(detail.Linkable))
	}

	archived := e.do("POST", "/api/tasks/"+taskID+"/archive", nil, jsonHeader)
	readBody(t, archived)
	if archived.StatusCode != 200 {
		t.Fatalf("archive a task: status %d", archived.StatusCode)
	}
	if left := taskList(t, e, projectID); len(left) != 0 {
		t.Fatalf("an archived task is still on the board: %+v", left)
	}
}
