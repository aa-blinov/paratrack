package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"testing"
)

// The API names who did the time, the project and task it went to, the project's client, the
// task's description, and the caller and the members with their roles. A member's token sees
// only its own sessions and no member list (ADM-12).
func TestAPIv1NamesTheWorkAndThePeople(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("api-v1-owner@x.test")
	member := newAPIEnvSharedDB(t, owner)
	member.register("api-v1-member@x.test")
	joinStudio(t, owner, map[string]*apiEnv{"api-v1-member@x.test": member}, "api-v1-member@x.test")

	ownerAuth := map[string]string{"Authorization": "Bearer " + apiTokenOf(t, owner, "v1-owner")}
	memberAuth := map[string]string{"Authorization": "Bearer " + apiTokenOf(t, member, "v1-member")}

	var me struct {
		Email  string `json:"email"`
		TeamID int64  `json:"team_id"`
		Role   string `json:"role"`
	}
	mustDecode(t, readBody(t, owner.do("GET", "/api/v1/me", nil, ownerAuth)), &me)
	if me.Email != "api-v1-owner@x.test" || me.Role != "owner" || me.TeamID == 0 {
		t.Fatalf("/api/v1/me answered %+v, want the owner in their workspace", me)
	}

	members := owner.do("GET", "/api/v1/members", nil, ownerAuth)
	var listed struct {
		Members []struct {
			Email string `json:"email"`
			Role  string `json:"role"`
		} `json:"members"`
	}
	mustDecode(t, readBody(t, members), &listed)
	roles := map[string]string{}
	for _, m := range listed.Members {
		roles[m.Email] = m.Role
	}
	if members.StatusCode != http.StatusOK || roles["api-v1-owner@x.test"] != "owner" || roles["api-v1-member@x.test"] != "member" {
		t.Fatalf("/api/v1/members for the owner: status %d, roles %v", members.StatusCode, roles)
	}
	denied := member.do("GET", "/api/v1/members", nil, memberAuth)
	readBody(t, denied)
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("a member's token listed the members: status %d, want 403", denied.StatusCode)
	}

	// A project with a client id, and a task with its description, in the owner's workspace.
	readBody(t, owner.do("POST", "/projects/new", url.Values{"name": {"API проект"}, "color": {"#7c3aed"}}, nil))
	var projectID int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM projects WHERE name = 'API проект'`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	var projects struct {
		Projects []struct {
			ClientID *int64 `json:"client_id"`
		} `json:"projects"`
	}
	mustDecode(t, readBody(t, owner.do("GET", "/api/v1/projects", nil, ownerAuth)), &projects)
	if len(projects.Projects) == 0 || projects.Projects[0].ClientID == nil {
		t.Fatal("the project answer has no client id")
	}
	readBody(t, owner.do("POST", "/api/tasks", url.Values{
		"title": {"Сверстать лендинг"}, "description": {"Макет утверждён, ждём тексты"}, "project_id": {strconv.FormatInt(projectID, 10)},
	}, nil))
	var tasks struct {
		Tasks []struct {
			Description string `json:"description"`
		} `json:"tasks"`
	}
	mustDecode(t, readBody(t, owner.do("GET", "/api/v1/tasks", nil, ownerAuth)), &tasks)
	if len(tasks.Tasks) == 0 || tasks.Tasks[0].Description != "Макет утверждён, ждём тексты" {
		t.Fatalf("the task answer carries no description: %+v", tasks.Tasks)
	}

	// A timer on the task's activity: the session names its author, project and task.
	started := owner.do("POST", "/api/start", url.Values{"activity": {"Сверстать лендинг"}}, map[string]string{"HX-Request": "true"})
	readBody(t, started)
	var sessionID int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM sessions ORDER BY id DESC LIMIT 1`).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	readBody(t, owner.do("POST", "/api/sessions/"+strconv.FormatInt(sessionID, 10)+"/stop", nil, nil))
	var taskID int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM tasks WHERE title = 'Сверстать лендинг'`).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	var sessions struct {
		Sessions []struct {
			ID        int64  `json:"id"`
			UserEmail string `json:"user_email"`
			ProjectID int64  `json:"project_id"`
			TaskID    int64  `json:"task_id"`
		} `json:"sessions"`
	}
	mustDecode(t, readBody(t, owner.do("GET", "/api/v1/sessions", nil, ownerAuth)), &sessions)
	found := false
	for _, s := range sessions.Sessions {
		if s.ID == sessionID {
			found = true
			if s.UserEmail != "api-v1-owner@x.test" || s.ProjectID != projectID || s.TaskID != taskID {
				t.Fatalf("the session names %+v, want the owner, project %d and task %d", s, projectID, taskID)
			}
		}
	}
	if !found {
		t.Fatalf("the session %d is missing from the API: %+v", sessionID, sessions.Sessions)
	}
}

// apiTokenOf mints an API token for the signed-in person and returns its raw value.
func apiTokenOf(t *testing.T, e *apiEnv, name string) string {
	t.Helper()
	page := readBody(t, e.do("POST", "/api/tokens", url.Values{"name": {name}}, nil))
	var raw string
	for _, candidate := range regexp.MustCompile(`pt_[A-Za-z0-9_-]+`).FindAllString(page, -1) {
		if len(candidate) > len(raw) {
			raw = candidate
		}
	}
	if raw == "" {
		t.Fatalf("the token %s is not shown on the page", name)
	}
	return mustUnescape(t, raw)
}

func mustDecode(t *testing.T, body string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(body), v); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
}
