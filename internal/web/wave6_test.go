package web

import (
	"net/url"
	"strings"
	"testing"
	"time"
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
	d.CreateClosedSession(ctx, 1, act.ID, start, end, "")

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
