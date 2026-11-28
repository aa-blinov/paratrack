package web

import (
	"encoding/json"
	"net/url"
	"strconv"
	"testing"
)

// A report narrowed to one project, or to one client's projects, holds only
// that work and names its scope: the document a PM sends the client.
func TestReportScopedToAProjectOrClient(t *testing.T) {
	e := newAPIEnv(t)
	e.register("report-scope@x.test")
	e.do("POST", "/api/team/modules", url.Values{"preset": {"studio"}, "from": {"welcome"}}, nil).Body.Close()
	ids := map[string]int64{}
	for _, name := range []string{"Альфа", "Бета"} {
		body := readBody(t, e.do("POST", "/api/projects", url.Values{"name": {name}, "color": {"#2563eb"}}, map[string]string{"Accept": "application/json"}))
		var p struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			t.Fatalf("project %s: %v (%s)", name, err, body)
		}
		ids[name] = p.ID
		e.do("POST", "/api/sessions/backfill", url.Values{"activity": {"Работа " + name}, "project_id": {strconv.FormatInt(p.ID, 10)}, "start": {"вчера 10:00"}, "end": {"вчера 12:00"}}, nil).Body.Close()
	}
	var clientID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(),
		`INSERT INTO clients (team_id, name) SELECT team_id, 'Ромашка' FROM projects WHERE id = ? RETURNING id`, ids["Бета"]).Scan(&clientID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.TestSQL().ExecContext(t.Context(), `UPDATE projects SET client_id = ? WHERE id = ?`, clientID, ids["Бета"]); err != nil {
		t.Fatal(err)
	}
	run := func(query string) reportRunPage {
		t.Helper()
		return reactData[reportRunPage](t, readBody(t, e.do("GET", "/reports/run?id=by-project&from=2020-01-01&to=2030-01-01"+query, nil, nil)))
	}
	keys := func(p reportRunPage) []string {
		var out []string
		for _, row := range p.VM.Rows {
			out = append(out, row.Key)
		}
		return out
	}
	if got := keys(run("")); len(got) != 2 {
		t.Fatalf("the whole workspace: %v", got)
	}
	project := run("&project_id=" + strconv.FormatInt(ids["Альфа"], 10))
	if got := keys(project); len(got) != 1 || got[0] != "Альфа" || project.VM.Scope == "" {
		t.Fatalf("one project: %v scope %q", got, project.VM.Scope)
	}
	client := run("&client_id=" + strconv.FormatInt(clientID, 10))
	if got := keys(client); len(got) != 1 || got[0] != "Бета" {
		t.Fatalf("one client: %v", got)
	}
	// Another workspace's project id is not taken: the report stays whole.
	if got := keys(run("&project_id=999999")); len(got) != 2 {
		t.Fatalf("a foreign project id narrowed the report: %v", got)
	}
}
