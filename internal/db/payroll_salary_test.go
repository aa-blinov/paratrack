package db

import (
	"context"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// payrollLines reads one run's lines, priced, as the list shows them.
func payrollLines(t *testing.T, d *DB, ctx context.Context, runID int64) []model.PayrollLine {
	t.Helper()
	rows, err := d.TestSQL().QueryContext(ctx,
		`SELECT user_id, seconds, rate_cents, amount_cents FROM payroll_lines WHERE run_id = ? ORDER BY user_id`, runID)
	if err != nil {
		t.Fatalf("read payroll lines: %v", err)
	}
	defer rows.Close()
	var out []model.PayrollLine
	for rows.Next() {
		var l model.PayrollLine
		if err := rows.Scan(&l.UserID, &l.Seconds, &l.RateCents, &l.AmountCents); err != nil {
			t.Fatal(err)
		}
		out = append(out, l)
	}
	return out
}

// The pay kind (FIN-26) round-trips: the manager switches a member to a
// monthly salary with an amount, the roster reads both back, and an unknown
// kind is refused before it reaches the row.
func TestMemberPayKindRoundTrips(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ownerID := seedApprovalUser(t, d, "salary-owner@test.local")
	memberID := seedApprovalUser(t, d, "salaried@test.local")
	teamID, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: "kinds", Name: "Команда", OwnerID: ownerID})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at, hourly_pay_cents) VALUES (?, ?, 'member', ?, 2500)`,
		teamID, memberID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatalf("join member: %v", err)
	}
	monthly, kind := 90000, "monthly"
	err = d.SetMemberPay(ctx, appmodel.PayrollMemberPayRequest{
		TeamID: teamID, UserID: memberID, CallerID: ownerID,
		Kind: &kind, MonthlyPayCents: &monthly, PayCents: ptrInt(0),
	})
	if err != nil {
		t.Fatalf("switch to the salary: %v", err)
	}
	read := func() model.MemberPayrollSettings {
		t.Helper()
		settings, err := d.ListMemberPayrollSettings(ctx, teamID)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range settings {
			if item.UserID == memberID {
				return item
			}
		}
		t.Fatalf("member %d is not on the roster", memberID)
		return model.MemberPayrollSettings{}
	}
	if got := read(); got.Kind != "monthly" || got.MonthlyPayCents != 90000 || got.PayCents != 0 {
		t.Fatalf("settings = %+v, want the monthly salary", got)
	}
	// A back-row member without an explicit kind is hourly, as always.
	settings, _ := d.ListMemberPayrollSettings(ctx, teamID)
	for _, item := range settings {
		if item.UserID == ownerID && item.Kind != "hourly" {
			t.Fatalf("default kind = %q, want hourly", item.Kind)
		}
	}
	bad := "weekly"
	if err := d.SetMemberPay(ctx, appmodel.PayrollMemberPayRequest{
		TeamID: teamID, UserID: memberID, CallerID: ownerID, Kind: &bad,
	}); err == nil {
		t.Fatal("an unknown pay kind was saved")
	}
	// Back to hourly keeps the stored salary: switching again brings it back.
	hourly := "hourly"
	if err := d.SetMemberPay(ctx, appmodel.PayrollMemberPayRequest{TeamID: teamID, UserID: memberID, CallerID: ownerID, Kind: &hourly}); err != nil {
		t.Fatalf("switch back: %v", err)
	}
	if got := read(); got.Kind != "hourly" || got.MonthlyPayCents != 90000 || got.PayCents != 0 {
		t.Fatalf("settings after the switch back = %+v, want hourly with the salary kept", got)
	}
}

func ptrInt(n int) *int { return &n }

// A salaried member (FIN-26) is paid the salary per calendar month the run
// covers, without tracked time, and their hours are not priced on top.
func TestPayRunPaysTheMonthlySalary(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ownerID := seedApprovalUser(t, d, "salary-run-owner@test.local")
	salariedID := seedApprovalUser(t, d, "salary-run-face@test.local")
	hourlyID := seedApprovalUser(t, d, "salary-run-hours@test.local")
	teamID, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: "salary-run", Name: "Команда", OwnerID: ownerID})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at, pay_kind, monthly_pay_cents) VALUES (?, ?, 'member', ?, 'monthly', 120000)`,
		teamID, salariedID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatalf("join the salaried member: %v", err)
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at, hourly_pay_cents) VALUES (?, ?, 'member', ?, 2500)`,
		teamID, hourlyID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatalf("join the hourly member: %v", err)
	}
	var activityID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO activities (name, team_id) VALUES ('Дизайн', ?) RETURNING id`, teamID).Scan(&activityID); err != nil {
		t.Fatalf("activity: %v", err)
	}
	// Two finished weeks of October 2026, approved, tracked by the hourly
	// member only; the salaried one tracks nothing (and still gets paid).
	week1 := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	seedClosedSession(t, d, teamID, hourlyID, activityID, week1.Add(10*time.Hour))
	ownerCtx := requestctx.WithActor(ctx, ownerID)
	if err := d.ApproveTimesheetWeek(ownerCtx, appmodel.TimesheetApprovalRequest{TeamID: teamID, CallerID: ownerID, Start: week1, End: week1.AddDate(0, 0, 7)}); err != nil {
		t.Fatalf("approve the week: %v", err)
	}
	// A salaried member with tracked time: seed one closed session to prove
	// no hourly line rides on top of the salary.
	seedClosedSession(t, d, teamID, salariedID, activityID, week1.Add(11*time.Hour))

	run, _, err := d.CreatePayrollDraft(ownerCtx, appmodel.PayrollDraftRequest{
		TeamID: teamID, CallerID: ownerID, Start: week1, End: week1.AddDate(0, 0, 7), CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("run over the approved week: %v", err)
	}
	type line struct {
		userID  int64
		seconds int64
		amount  int64
	}
	seen := map[int64]line{}
	for _, l := range payrollLines(t, d, ctx, run.ID) {
		seen[l.UserID] = line{userID: l.UserID, seconds: int64(l.Seconds), amount: int64(l.AmountCents)}
	}
	if got := seen[salariedID]; got.userID == 0 || got.amount != 120000 || got.seconds != 0 {
		t.Fatalf("salaried line = %v, want 0 hours for 120000", got)
	}
	if got := seen[hourlyID]; got.amount != 2500 {
		t.Fatalf("hourly line = %v, want 2500 for the tracked hour at 2500", got)
	}

	// A run covering two months closes two salaries in one document.
	novWeek := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	seedClosedSession(t, d, teamID, hourlyID, activityID, novWeek.Add(10*time.Hour))
	if err := d.ApproveTimesheetWeek(ownerCtx, appmodel.TimesheetApprovalRequest{TeamID: teamID, CallerID: ownerID, Start: novWeek, End: novWeek.AddDate(0, 0, 7)}); err != nil {
		t.Fatalf("approve the November week: %v", err)
	}
	run2, _, err := d.CreatePayrollDraft(ownerCtx, appmodel.PayrollDraftRequest{
		TeamID: teamID, CallerID: ownerID, Start: week1, End: novWeek.AddDate(0, 0, 7), CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("run across the two months: %v", err)
	}
	for _, l := range payrollLines(t, d, ctx, run2.ID) {
		if l.UserID == salariedID && l.AmountCents != 240000 {
			t.Fatalf("salaried line across two months = %d, want 240000", l.AmountCents)
		}
	}

	// A workspace of salaries only still creates a run without tracked time.
	freshOwner := seedApprovalUser(t, d, "salary-only@test.local")
	face := seedApprovalUser(t, d, "salary-only-face@test.local")
	fresh, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: "salary-only", Name: "Оклады", OwnerID: freshOwner})
	if err != nil {
		t.Fatalf("create the salaried workspace: %v", err)
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at, pay_kind, monthly_pay_cents) VALUES (?, ?, 'member', ?, 'monthly', 90000)`,
		fresh, face, FormatTime(time.Now().UTC())); err != nil {
		t.Fatalf("join: %v", err)
	}
	if _, _, err := d.CreatePayrollDraft(requestctx.WithActor(ctx, freshOwner), appmodel.PayrollDraftRequest{
		TeamID: fresh, CallerID: freshOwner, Start: week1, End: week1.AddDate(0, 0, 7), CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("a run of salaries only = %v, want the salary paid without tracked time", err)
	}
}
