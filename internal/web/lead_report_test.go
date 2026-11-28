package web

import (
	"net/url"
	"strconv"
	"testing"
)

// A project's lead opens the project's report, hours only; another member
// does not, and the lead cannot open it for a project they do not lead.
func TestProjectLeadOpensTheProjectReportWithoutMoney(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("own@lead.test")
	lead := newAPIEnvSharedDB(t, owner)
	lead.register("lead@lead.test")
	dev := newAPIEnvSharedDB(t, owner)
	dev.register("dev@lead.test")
	ids := joinStudio(t, owner, map[string]*apiEnv{"lead@lead.test": lead, "dev@lead.test": dev}, "lead@lead.test", "dev@lead.test")
	readBody(t, owner.do("POST", "/projects/new", url.Values{"name": {"Лид-проект"}, "rate": {"3000"}}, nil))
	readBody(t, owner.do("POST", "/projects/new", url.Values{"name": {"Чужой"}, "rate": {"3000"}}, nil))
	sql := owner.db.TestSQL()
	var mine, other int64
	sql.QueryRowContext(t.Context(), `SELECT id FROM projects WHERE name = 'Лид-проект'`).Scan(&mine)
	sql.QueryRowContext(t.Context(), `SELECT id FROM projects WHERE name = 'Чужой'`).Scan(&other)
	if _, err := sql.ExecContext(t.Context(), `UPDATE projects SET lead_user_id = ? WHERE id = ?`, ids[0], mine); err != nil {
		t.Fatal(err)
	}
	get := func(e *apiEnv, project int64) int {
		resp := e.do("GET", "/reports/run?id=by-project&project_id="+strconv.FormatInt(project, 10), nil, nil)
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := get(lead, mine); code != 200 {
		t.Fatalf("the lead's own project report: %d", code)
	}
	if code := get(lead, other); code == 200 {
		t.Fatal("the lead opened a project they do not lead")
	}
	if code := get(dev, mine); code == 200 {
		t.Fatal("a member opened the report")
	}
	page := readBody(t, lead.do("GET", "/reports/run?id=by-project&project_id="+strconv.FormatInt(mine, 10), nil, nil))
	if data := reactData[reportRunPage](t, page); data.VM.Template.Billable {
		t.Fatal("the lead's report shows the money columns")
	}
}
