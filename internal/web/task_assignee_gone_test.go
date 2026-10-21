package web

import (
	"encoding/json"
	"net/url"
	"strconv"
	"testing"
	"time"
)

// A task keeps the assignee who left the team, and the list says so instead of
// showing the task as unassigned. The reference itself is not cleared.
func TestTaskAssigneeWhoLeftTheTeamIsFlagged(t *testing.T) {
	e := newAPIEnv(t)
	e.register("assignee-gone@x.test")
	ctx := t.Context()
	jsonHeader := map[string]string{"Accept": "application/json"}

	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Assignee work"}}, nil))
	page := reactData[tasksPage](t, readBody(t, e.do("GET", "/tasks", nil, nil)))
	if len(page.Projects) != 1 {
		t.Fatalf("tasks page has %d projects, want 1", len(page.Projects))
	}
	projectID := strconv.FormatInt(page.Projects[0].ID, 10)

	var teamID, ownerID int64
	if err := e.db.TestSQL().QueryRowContext(ctx, `SELECT id FROM teams ORDER BY id LIMIT 1`).Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	if err := e.db.TestSQL().QueryRowContext(ctx, `SELECT id FROM users WHERE email = 'assignee-gone@x.test'`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	var formerID int64
	if err := e.db.TestSQL().QueryRowContext(ctx,
		`INSERT INTO users (email, password_hash, name) VALUES ('assignee-former@x.test', 'x', 'Ушедший Исполнитель') RETURNING id`).Scan(&formerID); err != nil {
		t.Fatalf("seed the member: %v", err)
	}
	if _, err := e.db.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, formerID, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("join the member: %v", err)
	}

	create := func(title string, assignee int64) int64 {
		t.Helper()
		form := url.Values{"project_id": {projectID}, "title": {title}}
		if assignee > 0 {
			form.Set("assignee_id", strconv.FormatInt(assignee, 10))
		}
		resp := e.do("POST", "/api/tasks", form, jsonHeader)
		body := readBody(t, resp)
		if resp.StatusCode != 200 {
			t.Fatalf("create %q: status %d, body %s", title, resp.StatusCode, body)
		}
		var made taskCreatedResponse
		if err := json.Unmarshal([]byte(body), &made); err != nil || made.ID == 0 {
			t.Fatalf("create %q: no id in %s (%v)", title, body, err)
		}
		return made.ID
	}
	formerTask := create("Left behind", formerID)
	openTask := create("Nobody yet", 0)
	mineTask := create("Mine", ownerID)

	removed := e.do("POST", "/api/team/members/"+strconv.FormatInt(formerID, 10)+"/remove", url.Values{}, nil)
	removed.Body.Close()
	if removed.StatusCode != 303 {
		t.Fatalf("remove member: status %d, want a redirect", removed.StatusCode)
	}

	list := readBody(t, e.do("GET", "/api/tasks", nil, jsonHeader))
	var got tasksListResponse
	if err := json.Unmarshal([]byte(list), &got); err != nil {
		t.Fatalf("decode the task list: %v", err)
	}
	byID := map[int64]taskItem{}
	for _, item := range got.Tasks {
		byID[item.ID] = item
	}

	left := byID[formerTask]
	if !left.AssigneeGone {
		t.Fatalf("task of a former member is not flagged: %+v", left)
	}
	if left.AssigneeID != formerID {
		t.Fatalf("removing the member cleared the assignee: got %d, want %d", left.AssigneeID, formerID)
	}
	if open := byID[openTask]; open.AssigneeGone || open.AssigneeID != 0 {
		t.Fatalf("unassigned task reads as a departed assignee: %+v", open)
	}
	if mine := byID[mineTask]; mine.AssigneeGone || mine.AssigneeName == "" {
		t.Fatalf("task of a member in the team is flagged or unnamed: %+v", mine)
	}
}
