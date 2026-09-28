package web

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/db"
)

func TestPayrollAndSchedule(t *testing.T) {
	d, err := newTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	d.SQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','Alice')`)
	d.SQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)
	d.SQL().ExecContext(ctx, `INSERT INTO memberships (team_id, user_id, role) VALUES (1,1,'owner')`)

	// pay rate 100.00/h
	pay, cap := 10000, 480
	if err := d.SetMemberPay(ctx, 1, 1, &pay, &cap); err != nil {
		t.Fatal(err)
	}
	p, c, err := d.MemberPay(ctx, 1, 1)
	if err != nil || p != 10000 || c != 480 {
		t.Fatalf("pay=%d cap=%d err=%v", p, c, err)
	}

	// 2h tracked
	act, _ := d.CreateActivity(ctx, 1, "work")
	start := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	d.CreateClosedSession(db.WithActor(ctx, 1), 1, act.ID, start, end, "")

	lines, err := d.BuildPayrollLines(ctx, 1, start.Add(-time.Hour), end.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("lines=%+v", lines)
	}
	if lines[0].AmountCents != 20000 || lines[0].UserID != 1 {
		t.Fatalf("line=%+v", lines[0])
	}

	num, _ := d.NextPayrollNumber(ctx, 1)
	if !strings.HasPrefix(num, "PAY-") {
		t.Fatalf("num=%s", num)
	}
	run, err := d.CreatePayrollRun(ctx, 1, num, "sept", start, end, lines)
	if err != nil {
		t.Fatal(err)
	}
	rl, _ := d.ListPayrollLines(ctx, run.ID)
	if len(rl) != 1 || rl[0].AmountCents != 20000 {
		t.Fatalf("rl=%+v", rl)
	}

	// schedule upsert
	proj, _ := d.CreateProject(ctx, 1, "Acme", "", "#7c3aed")
	day := "2026-09-22"
	if err := d.UpsertScheduleEntry(ctx, 1, 1, proj.ID, day, 240, ""); err != nil {
		t.Fatal(err)
	}
	monday := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	rows, pnames, err := d.ListSchedule(ctx, 1, monday)
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
	if err := d.UpsertScheduleEntry(ctx, 1, 1, proj.ID, day, 0, ""); err != nil {
		t.Fatal(err)
	}
	rows, _, _ = d.ListSchedule(ctx, 1, monday)
	if rows[0].Minutes[1] != 0 {
		t.Fatalf("not cleared: %+v", rows[0])
	}
}

func TestPayrollAPI(t *testing.T) {
	e := newAPIEnv(t)
	e.register("payroll@x.test")
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
	resp.Body.Close()
}

// A second pay run over days already paid asks first, then goes through.
func TestPayrollOverlapWarns(t *testing.T) {
	e := newAPIEnv(t)
	e.register("payover@x.test")
	htmx := map[string]string{"HX-Request": "true"}
	var uid string
	e.srv.db.SQL().QueryRowContext(t.Context(), `SELECT id FROM users WHERE email = 'payover@x.test'`).Scan(&uid)
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
	if loc := create(nil); !strings.HasPrefix(loc, "/payroll/") {
		t.Fatalf("first run: %q", loc)
	}
	loc := create(nil)
	if !strings.Contains(loc, "overlap=") {
		t.Fatalf("overlapping run created without a warning: %q", loc)
	}
	if page := readBody(t, e.do("GET", loc, nil, nil)); !strings.Contains(page, `name="confirm"`) || !strings.Contains(page, "PAY-") {
		t.Error("warning page has no confirm button or run number")
	}
	var n int
	e.srv.db.SQL().QueryRowContext(t.Context(), `SELECT count(*) FROM payroll_runs`).Scan(&n)
	if n != 1 {
		t.Fatalf("%d runs after the warning, want 1", n)
	}
	if loc := create(url.Values{"confirm": {"1"}}); !strings.HasPrefix(loc, "/payroll/") {
		t.Fatalf("confirmed run: %q", loc)
	}
}
