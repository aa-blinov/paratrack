package web

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/db"
)

func TestNewProjectLeadsToTimeTrackingWithProjectSelected(t *testing.T) {
	e := newAPIEnv(t)
	e.register("project-next-step@x.test")
	created := e.do("POST", "/projects/new", url.Values{"name": {"Client work"}, "rate": {"1500"}}, nil)
	projectURL := created.Header.Get("Location")
	created.Body.Close()
	if !strings.HasPrefix(projectURL, "/projects/") {
		t.Fatalf("project creation did not open its detail: %q", projectURL)
	}
	page := readBody(t, e.do("GET", projectURL, nil, nil))
	if !strings.Contains(page, `href="/?project=1"`) {
		t.Fatal("new project detail has no direct path to start tracking for this project")
	}
	start := readBody(t, e.do("GET", "/?project=1", nil, nil))
	backfill := strings.Index(start, `id="b-project"`)
	if !strings.Contains(start, `id="project_id"`) || !strings.Contains(start, `data-default="1"`) || backfill < 0 ||
		!strings.Contains(strings.SplitN(start[backfill:], "</select>", 2)[0], `<option value="1" selected>Client work</option>`) {
		t.Fatal("project handoff did not preselect the project for live and past time")
	}
	other := newAPIEnvSharedDB(t, e)
	other.register("project-other-team@x.test")
	readBody(t, other.do("POST", "/projects/new", url.Values{"name": {"Private project"}}, nil))
	unknown := readBody(t, e.do("GET", "/?project=2", nil, nil))
	if strings.Contains(unknown, `data-default="2"`) {
		t.Fatal("another workspace's project could be preselected")
	}
}

func TestEmptyScheduleOffersProjectWithoutScrollingTheGrid(t *testing.T) {
	e := newAPIEnv(t)
	e.register("empty-plan@x.test")
	page := readBody(t, e.do("GET", "/schedule", nil, nil))
	if strings.Contains(page, `class="week-grid `) || !strings.Contains(page, `href="/projects/new"`) {
		t.Fatal("empty schedule hides its next action inside a horizontally scrolled grid")
	}
	member := newAPIEnvSharedDB(t, e)
	member.register("empty-plan-member@x.test")
	joinStudio(t, e, map[string]*apiEnv{"empty-plan-member@x.test": member}, "empty-plan-member@x.test")
	memberPage := readBody(t, member.do("GET", "/schedule", nil, nil))
	if strings.Contains(memberPage, `href="/projects/new"`) || !strings.Contains(memberPage, "Попросите владельца") {
		t.Fatal("member sees a project creation action they cannot use")
	}
}

func TestScheduleWithOnlyOwnerLinksToInvitations(t *testing.T) {
	e := newAPIEnv(t)
	e.register("first-plan@x.test")
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Studio work"}}, nil))
	page := readBody(t, e.do("GET", "/schedule", nil, nil))
	if !strings.Contains(page, `class="week-grid `) || !strings.Contains(page, `href="/settings/invites"`) {
		t.Fatal("first schedule has no direct next step for adding teammates")
	}
}

func TestFirstInvoiceLinksStraightToCreateProject(t *testing.T) {
	e := newAPIEnv(t)
	e.register("first-bill@x.test")
	page := readBody(t, e.do("GET", "/invoices", nil, nil))
	if !strings.Contains(page, `href="/projects/new"`) {
		t.Fatal("invoices without any projects add an unnecessary projects-list stop")
	}
}

func TestGraphUsesStatsScopeAndPreservesItAcrossPeriods(t *testing.T) {
	e := newAPIEnv(t)
	e.register("graph-scope@x.test")
	htmx := map[string]string{"HX-Request": "true"}
	for _, name := range []string{"Client One", "Client Two"} {
		readBody(t, e.do("POST", "/projects/new", url.Values{"name": {name}, "rate": {"100"}}, nil))
	}
	day := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	for _, row := range []struct{ name, project string }{{"alpha-work", "1"}, {"beta-work", "2"}} {
		readBody(t, e.do("POST", "/api/sessions/backfill", url.Values{
			"activity": {row.name}, "start": {day + " 10:00"}, "end": {day + " 11:00"}, "project_id": {row.project},
		}, htmx))
	}
	var sessionID int64
	if err := e.srv.db.SQL().QueryRowContext(t.Context(), `SELECT s.id FROM sessions s JOIN activities a ON a.id=s.activity_id WHERE a.name='alpha-work'`).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	readBody(t, e.do("POST", fmt.Sprintf("/api/sessions/%d/tags", sessionID), url.Values{"name": {"review"}}, htmx))
	var slug string
	if err := e.srv.db.SQL().QueryRowContext(t.Context(), `SELECT slug FROM projects WHERE name='Client One'`).Scan(&slug); err != nil {
		t.Fatal(err)
	}
	scope := "period=yesterday&project=" + url.QueryEscape(slug) + "&tag=review"
	stats := readBody(t, e.do("GET", "/stats?"+scope, nil, nil))
	if !strings.Contains(stats, "/graph?period=yesterday") || !strings.Contains(stats, "tag=review") {
		t.Fatal("stats does not link to a graph with the same scope")
	}
	graph := readBody(t, e.do("GET", "/graph?"+scope, nil, nil))
	if !strings.Contains(graph, "alpha-work") || strings.Contains(graph, "beta-work") || !strings.Contains(graph, "Client One") {
		t.Fatal("graph ignores the project or tag filter")
	}
	if !strings.Contains(graph, "period=week") || !strings.Contains(graph, "tag=review") || !strings.Contains(graph, "project="+slug) {
		if i := strings.Index(graph, "period=week"); i >= 0 {
			t.Log(graph[i : i+min(200, len(graph)-i)])
		}
		t.Fatal("graph period links lost the scope")
	}
	noMatch := readBody(t, e.do("GET", "/graph?period=yesterday&project="+slug+"&tag=not-here", nil, nil))
	if strings.Contains(noMatch, "alpha-work") || !strings.Contains(noMatch, "not-here") {
		t.Fatal("an empty filtered graph was replaced by all-time data")
	}
}

func TestGraphPersonFilterHonorsTeamAndMemberScope(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("graph-owner@x.test")
	member := newAPIEnvSharedDB(t, owner)
	member.register("graph-member@x.test")
	ids := joinStudio(t, owner, map[string]*apiEnv{"graph-member@x.test": member}, "graph-member@x.test")
	day := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	for _, user := range []struct {
		env  *apiEnv
		name string
	}{{owner, "manager-work"}, {member, "member-work"}} {
		readBody(t, user.env.do("POST", "/api/sessions/backfill", url.Values{
			"activity": {user.name}, "start": {day + " 10:00"}, "end": {day + " 11:00"},
		}, map[string]string{"HX-Request": "true"}))
	}
	filtered := readBody(t, owner.do("GET", "/graph?period=yesterday&person="+ids[0], nil, nil))
	if !strings.Contains(filtered, "member-work") || strings.Contains(filtered, "manager-work") {
		t.Fatal("manager's graph ignored the person filter")
	}
	spoofed := readBody(t, member.do("GET", "/graph?period=yesterday&person=1", nil, nil))
	if strings.Contains(spoofed, "manager-work") || !strings.Contains(spoofed, "member-work") {
		t.Fatal("member saw another person's time")
	}
}

func TestUnassignedTimeCanBeAssignedBeforeBillingButNotAfter(t *testing.T) {
	e := newAPIEnv(t)
	e.register("invoice-repair@x.test")
	htmx := map[string]string{"HX-Request": "true"}
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Billable"}, "rate": {"100"}}, nil))
	day := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	readBody(t, e.do("POST", "/api/sessions/backfill", url.Values{
		"activity": {"unassigned-work"}, "start": {day + " 10:00"}, "end": {day + " 11:00"},
	}, htmx))
	var activityID int64
	if err := e.srv.db.SQL().QueryRowContext(t.Context(), `SELECT id FROM activities WHERE name='unassigned-work'`).Scan(&activityID); err != nil {
		t.Fatal(err)
	}
	form := readBody(t, e.do("GET", "/invoices", nil, nil))
	if !strings.Contains(form, "unassigned-work") || !strings.Contains(form, `name="confirm_history"`) {
		t.Fatal("invoice page hides unassigned time or its historical effect")
	}
	post := func(confirm, project string) *httpResult {
		resp := e.do("POST", "/invoices/assign", url.Values{
			"activity_id": {fmt.Sprint(activityID)}, "project_id": {project}, "confirm_history": {confirm},
		}, nil)
		defer resp.Body.Close()
		return &httpResult{resp.StatusCode, resp.Header.Get("Location")}
	}
	if resp := post("", "1"); resp.code != 400 {
		t.Fatalf("assignment without consent: %d", resp.code)
	}
	if resp := post("1", "99999"); resp.code != 303 || !strings.Contains(resp.loc, "flash=") {
		t.Fatalf("assignment to unknown project: %+v", resp)
	}
	if a, _ := e.srv.db.GetActivity(t.Context(), activityID); a.ProjectID != 0 {
		t.Fatal("failed assignment changed the activity")
	}
	if resp := post("1", "1"); resp.code != 303 || !strings.Contains(resp.loc, "project=1") {
		t.Fatalf("assignment failed: %+v", resp)
	}
	if a, _ := e.srv.db.GetActivity(t.Context(), activityID); a.ProjectID != 1 {
		t.Fatal("past sessions did not move with the activity")
	}
	bill := e.do("POST", "/invoices", url.Values{"project_id": {"1"}, "client": {"Customer"}, "start": {day}, "end": {day}}, nil)
	loc := bill.Header.Get("Location")
	bill.Body.Close()
	if !strings.HasPrefix(loc, "/invoices/") || strings.Contains(loc, "flash=") {
		t.Fatalf("past time was not invoiceable: %q", loc)
	}
	// The general project picker must also refuse changes after billing.
	resp := e.do("POST", fmt.Sprintf("/api/activities/%d/project", activityID), url.Values{"project_id": {"0"}}, nil)
	resp.Body.Close()
	if resp.StatusCode != 409 {
		t.Fatalf("billed activity moved through the project picker: %d", resp.StatusCode)
	}
	if err := e.srv.db.AssignActivityProject(t.Context(), 1, activityID, 0); !errors.Is(err, db.ErrAlreadyBilled) {
		t.Fatalf("billed activity was moved: %v", err)
	}
	// Simulate a legacy activity whose project was cleared before this guard.
	if _, err := e.srv.db.SQL().ExecContext(t.Context(), `UPDATE activities SET project_id = NULL WHERE id = ?`, activityID); err != nil {
		t.Fatal(err)
	}
	if resp := post("1", "1"); resp.code != 303 || !strings.Contains(resp.loc, "flash=") {
		t.Fatalf("billed activity was reattached: %+v", resp)
	}
	if a, _ := e.srv.db.GetActivity(t.Context(), activityID); a.ProjectID != 0 {
		t.Fatal("billed history changed project")
	}
	if page := readBody(t, e.do("GET", "/invoices", nil, nil)); strings.Contains(page, `name="activity_id" value="`+fmt.Sprint(activityID)+`"`) {
		t.Fatal("billed time offered as unassigned repair")
	}
}
