package web

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"
)

// A member follows a task themselves; only a manager adds someone else or names a co-assignee,
// and a member stops only their own watch (TASK-12).
func TestTaskWatchersFollowAndOnlyAManagerAddsOthers(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("watch-owner@x.test")
	member := newAPIEnvSharedDB(t, owner)
	member.register("watch-member@x.test")
	other := newAPIEnvSharedDB(t, owner)
	other.register("watch-other@x.test")
	joinStudio(t, owner, map[string]*apiEnv{"watch-member@x.test": member, "watch-other@x.test": other},
		"watch-member@x.test", "watch-other@x.test")

	readBody(t, owner.do("POST", "/projects/new", url.Values{"name": {"Согласование"}}, nil))
	readBody(t, owner.do("POST", "/api/tasks", url.Values{"project_id": {"1"}, "title": {"Согласовать смету"}}, map[string]string{"Accept": "application/json"}))
	var taskID, ownerID, memberID, otherID int64
	for query, dst := range map[string]*int64{
		`SELECT id FROM tasks WHERE title = 'Согласовать смету'`:   &taskID,
		`SELECT id FROM users WHERE email = 'watch-owner@x.test'`:  &ownerID,
		`SELECT id FROM users WHERE email = 'watch-member@x.test'`: &memberID,
		`SELECT id FROM users WHERE email = 'watch-other@x.test'`:  &otherID,
	} {
		if err := owner.db.TestSQL().QueryRowContext(t.Context(), query).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	task := "/api/tasks/" + strconv.FormatInt(taskID, 10) + "/watch"
	jsonHeader := map[string]string{"Accept": "application/json"}

	if r := member.do("POST", task, url.Values{"watch": {"1"}}, jsonHeader); r.StatusCode != http.StatusOK {
		t.Fatalf("a member could not follow the task: %d", r.StatusCode)
	} else {
		readBody(t, r)
	}
	detail := reactData[taskDetailPage](t, readBody(t, owner.do("GET", "/tasks/"+strconv.FormatInt(taskID, 10), nil, nil)))
	if len(detail.Watchers) != 1 || detail.Watchers[0].Role != "watcher" {
		t.Fatalf("the followers are %+v, want the member as a watcher", detail.Watchers)
	}

	addOther := member.do("POST", task, url.Values{"watch": {"1"}, "user_id": {strconv.FormatInt(otherID, 10)}, "role": {"co-assignee"}}, jsonHeader)
	readBody(t, addOther)
	if addOther.StatusCode != http.StatusForbidden {
		t.Fatalf("a member added someone else to a task: %d, want 403", addOther.StatusCode)
	}

	named := owner.do("POST", task, url.Values{"watch": {"1"}, "user_id": {strconv.FormatInt(otherID, 10)}, "role": {"co-assignee"}}, jsonHeader)
	readBody(t, named)
	if named.StatusCode != http.StatusOK {
		t.Fatalf("the manager could not name a co-assignee: %d", named.StatusCode)
	}
	detail = reactData[taskDetailPage](t, readBody(t, owner.do("GET", "/tasks/"+strconv.FormatInt(taskID, 10), nil, nil)))
	found := false
	for _, w := range detail.Watchers {
		if w.UserID == otherID && w.Role == "co-assignee" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the followers are %+v, want the other person as a co-assignee", detail.Watchers)
	}

	stopOther := member.do("POST", task, url.Values{"watch": {"0"}, "user_id": {strconv.FormatInt(otherID, 10)}}, jsonHeader)
	readBody(t, stopOther)
	if stopOther.StatusCode != http.StatusForbidden {
		t.Fatalf("a member stopped someone else's watch: %d, want 403", stopOther.StatusCode)
	}

	stopSelf := member.do("POST", task, url.Values{"watch": {"0"}}, jsonHeader)
	readBody(t, stopSelf)
	if stopSelf.StatusCode != http.StatusOK {
		t.Fatalf("a member could not stop their own watch: %d", stopSelf.StatusCode)
	}
	detail = reactData[taskDetailPage](t, readBody(t, owner.do("GET", "/tasks/"+strconv.FormatInt(taskID, 10), nil, nil)))
	for _, w := range detail.Watchers {
		if w.UserID == memberID {
			t.Fatalf("the member still follows the task: %+v", detail.Watchers)
		}
	}
}
