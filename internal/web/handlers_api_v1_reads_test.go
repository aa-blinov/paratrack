package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"testing"
	"time"
)

// A read-only token lists the clients, tags and task board of its workspace, in
// the public shape, and cannot write. These are what a move needs beside time.
func TestAPIv1ReadsListClientsTagsAndTasksForAReadOnlyToken(t *testing.T) {
	e := newAPIEnv(t)
	e.register("api-reads@x.test")
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Read work"}}, nil))
	page := reactData[tasksPage](t, readBody(t, e.do("GET", "/tasks", nil, nil)))
	projectID := strconv.FormatInt(page.Projects[0].ID, 10)
	jsonHeader := map[string]string{"Accept": "application/json"}
	if created := e.do("POST", "/api/tasks", url.Values{"project_id": {projectID}, "title": {"Read me"}}, jsonHeader); created.StatusCode != 200 {
		t.Fatalf("create a task: status %d", created.StatusCode)
	}
	var teamID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM teams ORDER BY id LIMIT 1`).Scan(&teamID); err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000")
	if _, err := e.db.TestSQL().ExecContext(t.Context(),
		`INSERT INTO clients (team_id, name, email, details, currency, notes, archived, created_at, updated_at) VALUES (?, 'Ромашка', 'buy@romashka.test', '', 'RUB', '', 0, ?, ?)`,
		teamID, now, now); err != nil {
		t.Fatalf("client: %v", err)
	}
	if _, err := e.db.TestSQL().ExecContext(t.Context(), `INSERT INTO tags (name, team_id, created_at) VALUES ('срочно', ?, ?)`, teamID, now); err != nil {
		t.Fatalf("tag: %v", err)
	}

	resp := e.do("POST", "/api/tokens", url.Values{"name": {"reader"}, "read_only": {"1"}}, nil)
	page2 := readBody(t, resp)
	var raw string
	for _, candidate := range regexp.MustCompile(`pt_[A-Za-z0-9_-]+`).FindAllString(page2, -1) {
		if len(candidate) > len(raw) {
			raw = candidate
		}
	}
	if raw == "" {
		t.Fatal("the read-only token is not shown on the page")
	}
	bearer := map[string]string{"Authorization": "Bearer " + mustUnescape(t, raw)}
	saved := e.jar
	e.jar = map[string]string{"paratrack_csrf": saved["paratrack_csrf"]}
	defer func() { e.jar = saved }()

	clients := e.do("GET", "/api/v1/clients", nil, bearer)
	var clientsOut apiV1ClientsResponse
	decodeBody(t, clients, &clientsOut)
	if clients.StatusCode != 200 || len(clientsOut.Clients) != 1 || clientsOut.Clients[0].Name != "Ромашка" || clientsOut.Clients[0].Email != "buy@romashka.test" {
		t.Fatalf("clients: status %d, got %+v", clients.StatusCode, clientsOut.Clients)
	}
	tags := e.do("GET", "/api/v1/tags", nil, bearer)
	var tagsOut apiV1TagsResponse
	decodeBody(t, tags, &tagsOut)
	if tags.StatusCode != 200 || len(tagsOut.Tags) != 1 || tagsOut.Tags[0].Name != "срочно" {
		t.Fatalf("tags: status %d, got %+v", tags.StatusCode, tagsOut.Tags)
	}
	tasks := e.do("GET", "/api/v1/tasks?project="+projectID, nil, bearer)
	var tasksOut apiV1TasksResponse
	decodeBody(t, tasks, &tasksOut)
	if tasks.StatusCode != 200 || len(tasksOut.Tasks) != 1 || tasksOut.Tasks[0].Title != "Read me" || len(tasksOut.Tasks[0].Activities) != 1 || tasksOut.Tasks[0].TrackedMinutes != 0 {
		t.Fatalf("tasks: status %d, got %+v", tasks.StatusCode, tasksOut.Tasks)
	}

	write := e.do("POST", "/api/v1/sessions", url.Values{"activity": {"Read me"}}, bearer)
	readBody(t, write)
	if write.StatusCode == 200 || write.StatusCode == 201 {
		t.Fatalf("a read-only token wrote time: status %d", write.StatusCode)
	}
}

// decodeBody reads a JSON response into out and fails the test if it is not JSON.
func decodeBody(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	body := readBody(t, resp)
	if err := json.Unmarshal([]byte(body), out); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
}
