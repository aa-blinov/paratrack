package web

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/audit"
	"github.com/aa-blinov/paratrack/internal/payroll"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestPayrollAndSchedule(t *testing.T) {
	d, err := newTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	d.TestSQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','Alice')`)
	d.TestSQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)
	d.TestSQL().ExecContext(ctx, `INSERT INTO memberships (team_id, user_id, role) VALUES (1,1,'owner')`)
	ctx = requestctx.WithActor(ctx, 1)

	auditService, err := audit.New(d)
	if err != nil {
		t.Fatal(err)
	}
	payrollService, err := payroll.NewServiceWithClock(d, time.Now, auditService, log.Default())
	if err != nil {
		t.Fatal(err)
	}
	// Pay rate 100.00/h.
	pay, cap := 10000, 480
	if err := payrollService.UpdateMemberPay(ctx, appmodel.PayrollMemberPayRequest{
		TeamID: 1, UserID: 1, CallerID: 1, PayCents: &pay, CapacityMinutes: &cap,
	}); err != nil {
		t.Fatal(err)
	}
	settings, err := payrollService.MemberSettings(ctx, 1)
	if err != nil || len(settings) != 1 || settings[0].PayCents != 10000 || settings[0].CapacityMinutes != 480 {
		t.Fatalf("member settings=%+v err=%v", settings, err)
	}

	// 2h tracked
	act, _ := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: 1, CallerID: 1, Name: "work"})
	start := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	d.CreateClosedSession(requestctx.WithActor(ctx, 1), appmodel.TimerAddRequest{TeamID: 1, ActivityID: act.ID, Start: start, End: end})

	run, overlaps, err := payrollService.CreateRun(ctx, appmodel.PayrollDraftRequest{
		TeamID: 1, CallerID: 1, Notes: "sept", Start: start, End: end,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(overlaps) != 0 {
		t.Fatalf("unexpected payroll overlaps: %+v", overlaps)
	}
	runDetails, err := payrollService.GetRun(ctx, 1, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runDetails.Lines) != 1 || runDetails.Lines[0].AmountCents != 20000 {
		t.Fatalf("rl=%+v", runDetails.Lines)
	}

	// schedule upsert
	proj, _ := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: 1, CallerID: 1, Name: "Acme", Slug: "", Color: "#7c3aed"})
	day := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	if err := d.UpsertScheduleEntry(ctx, appmodel.ScheduleCellRequest{
		TeamID: 1, ActorID: 1, UserID: 1, ProjectID: proj.ID, Day: day, Minutes: 240,
	}); err != nil {
		t.Fatal(err)
	}
	monday := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	rows, pnames, err := d.ListSchedule(ctx, appmodel.ScheduleQuery{TeamID: 1, WeekStart: monday})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Minutes[1] != 240 {
		t.Fatalf("rows=%+v", rows)
	}
	if pnames[proj.ID] != "Acme" {
		t.Fatalf("pnames=%v", pnames)
	}
	// 0 clears
	if err := d.UpsertScheduleEntry(ctx, appmodel.ScheduleCellRequest{
		TeamID: 1, ActorID: 1, UserID: 1, ProjectID: proj.ID, Day: day,
	}); err != nil {
		t.Fatal(err)
	}
	rows, _, _ = d.ListSchedule(ctx, appmodel.ScheduleQuery{TeamID: 1, WeekStart: monday})
	if rows[0].Minutes[1] != 0 {
		t.Fatalf("not cleared: %+v", rows[0])
	}
}

func TestPayrollAPI(t *testing.T) {
	e := newAPIEnv(t)
	e.register("payroll@x.test")
	page := e.do("GET", "/payroll", nil, nil)
	if page.StatusCode != http.StatusOK || !strings.Contains(readBody(t, page), `"PayrollReact":true`) {
		t.Fatalf("payroll page did not bootstrap React: %d", page.StatusCode)
	}
	// set pay on self via members page is multi-step; just hit the generator
	// with no paid members → flash error, 303.
	resp := e.do("POST", "/payroll", url.Values{
		"start": {"2026-09-01"}, "end": {"2026-09-30"},
	}, nil)
	if resp.StatusCode != 303 {
		t.Fatalf("payroll: %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	resp.Body.Close()
	if !strings.Contains(loc, "/payroll") {
		t.Fatalf("loc=%s", loc)
	}
	// schedule page renders
	resp = e.do("GET", "/schedule", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("schedule: %d %s", resp.StatusCode, readBody(t, resp))
	}
	pageContent := readBody(t, resp)
	if !strings.Contains(pageContent, `"ScheduleReact":true`) || !strings.Contains(pageContent, `id="paratrack-react-root"`) {
		t.Fatalf("schedule page did not bootstrap React: %s", pageContent)
	}
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Scheduled work"}}, nil))
	var userID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM users WHERE email='payroll@x.test'`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	var projectID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM projects WHERE name='Scheduled work'`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	updated := e.do("POST", "/api/schedule/cell", url.Values{
		"user_id": {strconv.FormatInt(userID, 10)}, "project_id": {strconv.FormatInt(projectID, 10)}, "date": {"2026-09-21"}, "minutes": {"120"},
	}, map[string]string{"Accept": "application/json"})
	if updated.StatusCode != http.StatusOK || !strings.Contains(updated.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("schedule cell update: status=%d content-type=%q body=%s", updated.StatusCode, updated.Header.Get("Content-Type"), readBody(t, updated))
	}
	var result struct {
		Rows []schedRow `json:"rows"`
	}
	if err := json.NewDecoder(updated.Body).Decode(&result); err != nil {
		t.Fatalf("decode schedule update: %v", err)
	}
	updated.Body.Close()
	if len(result.Rows) != 1 || result.Rows[0].Cells[0].Min != 120 {
		t.Fatalf("schedule update response = %+v", result.Rows)
	}
}

// A second pay run over days already paid asks first, then goes through.
func TestPayrollOverlapWarns(t *testing.T) {
	e := newAPIEnv(t)
	e.register("payover@x.test")
	htmx := map[string]string{"HX-Request": "true"}
	var uid string
	e.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM users WHERE email = 'payover@x.test'`).Scan(&uid)
	resp := e.do("POST", "/api/member/pay", url.Values{"user_id": {uid}, "hourly_pay": {"1000"}}, nil)
	resp.Body.Close()
	readBody(t, e.do("POST", "/api/sessions/backfill", url.Values{"activity": {"вёрстка"}, "start": {"вчера 10:00"}, "end": {"вчера 12:00"}}, htmx))
	day := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	create := func(extra url.Values) string {
		v := url.Values{"start": {day}, "end": {day}, "notes": {"сентябрь"}}
		for k, x := range extra {
			v[k] = x
		}
		resp := e.do("POST", "/payroll", v, nil)
		resp.Body.Close()
		return resp.Header.Get("Location")
	}
	firstRun := create(nil)
	if !strings.HasPrefix(firstRun, "/payroll/") {
		t.Fatalf("first run: %q", firstRun)
	}
	if page := readBody(t, e.do("GET", firstRun, nil, nil)); !strings.Contains(page, `"PayrollDetail":true`) || !strings.Contains(page, `id="paratrack-react-root"`) {
		t.Fatal("payroll detail did not bootstrap React")
	}
	loc := create(nil)
	if !strings.Contains(loc, "overlap=") {
		t.Fatalf("overlapping run created without a warning: %q", loc)
	}
	if page := readBody(t, e.do("GET", loc, nil, nil)); !strings.Contains(reactData[payrollPage](t, page).Overlap, "PAY-") {
		t.Error("warning page has no confirm button or run number")
	}
	var n int
	e.db.TestSQL().QueryRowContext(t.Context(), `SELECT count(*) FROM payroll_runs`).Scan(&n)
	if n != 1 {
		t.Fatalf("%d runs after the warning, want 1", n)
	}
	if loc := create(url.Values{"confirm": {"1"}}); !strings.HasPrefix(loc, "/payroll/") {
		t.Fatalf("confirmed run: %q", loc)
	}
}
