package web

import (
	"net/url"
	"strconv"
	"testing"
	"time"
)

// The lead of a project plans that project on the schedule, and only that
// one. The page marks the lines the viewer may edit, and the server refuses a
// write to any other project.
func TestProjectLeadPlansOwnProjectOnly(t *testing.T) {
	e := newAPIEnv(t)
	e.register("lead-owner-web@x.test")
	member := newAPIEnvSharedDB(t, e)
	member.register("lead-member-web@x.test")
	// The studio is made first: projects belong to the workspace they are
	// created in, so Alpha and Beta are created inside it.
	joinStudio(t, e, map[string]*apiEnv{"lead-member-web@x.test": member}, "lead-member-web@x.test")
	for _, name := range []string{"Alpha", "Beta"} {
		readBody(t, e.do("POST", "/projects/new", url.Values{"name": {name}}, nil))
	}

	var memberID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM users WHERE email = 'lead-member-web@x.test'`).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	var alphaID, betaID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM projects WHERE name = 'Alpha'`).Scan(&alphaID); err != nil {
		t.Fatal(err)
	}
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM projects WHERE name = 'Beta'`).Scan(&betaID); err != nil {
		t.Fatal(err)
	}

	// The owner makes the member the lead of Alpha.
	resp := e.do("POST", "/projects/alpha", url.Values{
		"name": {"Alpha"}, "lead_user_id": {strconv.FormatInt(memberID, 10)},
	}, nil)
	resp.Body.Close()

	today := time.Now().Format("2006-01-02")
	cell := func(project int64) int {
		r := member.do("POST", "/api/schedule/cell", url.Values{
			"user_id": {strconv.FormatInt(memberID, 10)}, "project_id": {strconv.FormatInt(project, 10)},
			"date": {today}, "minutes": {"60"},
		}, map[string]string{"Accept": "application/json"})
		r.Body.Close()
		return r.StatusCode
	}
	if status := cell(alphaID); status != 200 {
		t.Fatalf("lead plans their project: status %d, want 200", status)
	}
	if status := cell(betaID); status != 403 {
		t.Fatalf("lead plans another project: status %d, want 403", status)
	}

	page := reactData[schedulePage](t, readBody(t, member.do("GET", "/schedule", nil, nil)))
	var alphaEditable, betaListed bool
	for _, person := range page.People {
		for _, line := range person.Lines {
			if line.ProjectID == alphaID && person.UserID == memberID {
				alphaEditable = line.CanEdit
			}
			if line.ProjectID == betaID && line.CanEdit {
				t.Fatal("a line the lead does not lead is marked editable")
			}
		}
	}
	for _, project := range page.EditableProjects {
		if project.ID == betaID {
			betaListed = true
		}
	}
	if !alphaEditable {
		t.Fatal("the lead's own project line is not marked editable")
	}
	if betaListed {
		t.Fatal("a project the lead does not lead is offered for adding")
	}
}
