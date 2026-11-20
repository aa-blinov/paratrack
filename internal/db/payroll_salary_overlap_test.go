package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// Two runs can price one calendar month without their periods meeting. The
// second run comes back as an overlap the manager confirms with a written
// reason, the same as re-paying hours (FIN-22), instead of paying the salary
// twice in silence.
func TestPayRunPricingTheSameMonthNeedsConfirmation(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ownerID := seedApprovalUser(t, d, "salary-overlap-owner@test.local")
	faceID := seedApprovalUser(t, d, "salary-overlap-face@test.local")
	teamID, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: "salary-overlap", Name: "Команда", OwnerID: ownerID})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at, pay_kind, monthly_pay_cents) VALUES (?, ?, 'member', ?, 'monthly', 120000)`,
		teamID, faceID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatalf("join the salaried member: %v", err)
	}
	var activityID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO activities (name, team_id) VALUES ('Дизайн', ?) RETURNING id`, teamID).Scan(&activityID); err != nil {
		t.Fatalf("activity: %v", err)
	}
	ownerCtx := requestctx.WithActor(ctx, ownerID)
	week1 := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	week2 := week1.AddDate(0, 0, 7)
	seedClosedSession(t, d, teamID, ownerID, activityID, week1.Add(10*time.Hour))
	for _, week := range []time.Time{week1, week2} {
		if err := d.ApproveTimesheetWeek(ownerCtx, appmodel.TimesheetApprovalRequest{TeamID: teamID, CallerID: ownerID, Start: week, End: week.AddDate(0, 0, 7)}); err != nil {
			t.Fatalf("approve the week of %s: %v", week.Format("2006-01-02"), err)
		}
	}

	first, _, err := d.CreatePayrollDraft(ownerCtx, appmodel.PayrollDraftRequest{
		TeamID: teamID, CallerID: ownerID, Start: week1, End: week2, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("the month's first run: %v", err)
	}

	// The second week of the same month does not meet the first run's
	// period, but both price October's salary.
	_, overlaps, err := d.CreatePayrollDraft(ownerCtx, appmodel.PayrollDraftRequest{
		TeamID: teamID, CallerID: ownerID, Start: week2, End: week2.AddDate(0, 0, 7), CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("the month's second run: %v", err)
	}
	if len(overlaps) == 0 {
		t.Fatal("a second run in the same salary month passed without confirmation")
	}
	if overlaps[0].ID != first.ID {
		t.Fatalf("overlap names run %d, want %d", overlaps[0].ID, first.ID)
	}

	// With the confirmation the run is created and pays the month again.
	second, _, err := d.CreatePayrollDraft(ownerCtx, appmodel.PayrollDraftRequest{
		TeamID: teamID, CallerID: ownerID, Start: week2, End: week2.AddDate(0, 0, 7), CreatedAt: time.Now().UTC(), ConfirmOverlap: true,
	})
	if err != nil {
		t.Fatalf("the confirmed second run: %v", err)
	}
	for _, l := range payrollLines(t, d, ctx, second.ID) {
		if l.UserID == faceID && l.AmountCents != 120000 {
			t.Fatalf("the confirmed run pays %d, want the salary", l.AmountCents)
		}
	}
}

// A member who left keeps their pay kind: a run over the months they were
// present for still pays the salary, a month entirely after the departure
// pays nothing, and the return restores the kind with the amount.
func TestFormerMemberSalaryCoversOnlyMonthsPresent(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ownerID := seedApprovalUser(t, d, "former-salary-owner@test.local")
	faceID := seedApprovalUser(t, d, "former-salary-face@test.local")
	teamID, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: "former-salary", Name: "Команда", OwnerID: ownerID})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at, pay_kind, monthly_pay_cents) VALUES (?, ?, 'member', ?, 'monthly', 90000)`,
		teamID, faceID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatalf("join the salaried member: %v", err)
	}
	var activityID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO activities (name, team_id) VALUES ('Дизайн', ?) RETURNING id`, teamID).Scan(&activityID); err != nil {
		t.Fatalf("activity: %v", err)
	}
	ownerCtx := requestctx.WithActor(ctx, ownerID)
	// The owner carries an hourly rate, so a run after the departure still
	// has someone payable and the refusal under test is about the former
	// member's salary alone.
	if _, err := d.TestSQL().ExecContext(ctx,
		`UPDATE memberships SET hourly_pay_cents = 2500 WHERE team_id = ? AND user_id = ?`, teamID, ownerID); err != nil {
		t.Fatal(err)
	}
	october := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	november := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	seedClosedSession(t, d, teamID, ownerID, activityID, october.Add(10*time.Hour))
	seedClosedSession(t, d, teamID, ownerID, activityID, november.Add(10*time.Hour))
	for _, week := range []time.Time{october, november} {
		if err := d.ApproveTimesheetWeek(ownerCtx, appmodel.TimesheetApprovalRequest{TeamID: teamID, CallerID: ownerID, Start: week, End: week.AddDate(0, 0, 7)}); err != nil {
			t.Fatalf("approve the week of %s: %v", week.Format("2006-01-02"), err)
		}
	}

	// The departure lands mid-October: October is a month of presence,
	// November is not.
	leftAt := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	if err := d.RemoveTeamMember(ctx, appmodel.TeamMemberRemovalRequest{TeamID: teamID, TargetUserID: faceID, CallerID: ownerID, LeftAt: leftAt}); err != nil {
		t.Fatalf("remove the salaried member: %v", err)
	}

	octoberRun, _, err := d.CreatePayrollDraft(ownerCtx, appmodel.PayrollDraftRequest{
		TeamID: teamID, CallerID: ownerID, Start: october, End: october.AddDate(0, 0, 7), CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("run over the month of presence: %v", err)
	}
	paid := false
	for _, l := range payrollLines(t, d, ctx, octoberRun.ID) {
		if l.UserID == faceID {
			paid = true
			if l.AmountCents != 90000 {
				t.Fatalf("the month of presence pays %d, want the salary", l.AmountCents)
			}
		}
	}
	if !paid {
		t.Fatal("the month of presence pays the former member nothing")
	}

	novemberRun, _, err := d.CreatePayrollDraft(ownerCtx, appmodel.PayrollDraftRequest{
		TeamID: teamID, CallerID: ownerID, Start: november, End: november.AddDate(0, 0, 7), CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("run over the month after the departure: %v", err)
	}
	for _, l := range payrollLines(t, d, ctx, novemberRun.ID) {
		if l.UserID == faceID {
			t.Fatalf("the month after the departure pays a line: %+v", l)
		}
	}

	// The return restores the pay kind with the amount (ADM-04).
	if err := d.ReturnTeamMember(ctx, appmodel.TeamMemberReturnRequest{TeamID: teamID, TargetUserID: faceID, CallerID: ownerID}); err != nil {
		t.Fatalf("return the member: %v", err)
	}
	var kind string
	var monthly int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT pay_kind, monthly_pay_cents FROM memberships WHERE team_id = ? AND user_id = ?`, teamID, faceID).Scan(&kind, &monthly); err != nil {
		t.Fatal(err)
	}
	if kind != "monthly" || monthly != 90000 {
		t.Fatalf("returned pay = %s/%d, want the salary the member left with", kind, monthly)
	}
}
