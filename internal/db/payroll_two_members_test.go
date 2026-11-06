package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// Two members at different rates, two weeks, one approved and one not. The run
// over the approved week prices each person at their own rate, and the week
// that is still open cannot be paid.
func TestPayRunPricesEachMemberAtTheirOwnRateAcrossApprovedWeeks(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ownerID := seedApprovalUser(t, d, "two-owner@test.local")
	memberID := seedApprovalUser(t, d, "two-member@test.local")
	teamID, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: "two-members", Name: "Команда", OwnerID: ownerID})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at, hourly_pay_cents) VALUES (?, ?, 'member', ?, 2500)`,
		teamID, memberID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatalf("join member: %v", err)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE memberships SET hourly_pay_cents = 4000 WHERE team_id = ? AND user_id = ?`, teamID, ownerID); err != nil {
		t.Fatalf("owner rate: %v", err)
	}
	var activityID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO activities (name, team_id) VALUES ('Вёрстка', ?) RETURNING id`, teamID).Scan(&activityID); err != nil {
		t.Fatalf("activity: %v", err)
	}
	week1 := time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC)
	week2 := week1.AddDate(0, 0, 7)
	seedClosedSession(t, d, teamID, ownerID, activityID, week1.Add(10*time.Hour))
	seedClosedSession(t, d, teamID, memberID, activityID, week1.AddDate(0, 0, 1).Add(10*time.Hour))
	seedClosedSession(t, d, teamID, memberID, activityID, week1.AddDate(0, 0, 1).Add(12*time.Hour))
	seedClosedSession(t, d, teamID, ownerID, activityID, week2.Add(10*time.Hour))

	ownerCtx := requestctx.WithActor(ctx, ownerID)
	if err := d.ApproveTimesheetWeek(ownerCtx, appmodel.TimesheetApprovalRequest{TeamID: teamID, CallerID: ownerID, Start: week1, End: week1.AddDate(0, 0, 7)}); err != nil {
		t.Fatalf("approve week 1: %v", err)
	}

	// Week 2 is not approved, so a run that spans both weeks is refused.
	_, _, err = d.CreatePayrollDraft(ownerCtx, appmodel.PayrollDraftRequest{
		TeamID: teamID, CallerID: ownerID, Start: week1, End: week2.AddDate(0, 0, 7), CreatedAt: time.Now().UTC(),
	})
	var notApproved *model.PayrollNotApprovedError
	if !errors.As(err, &notApproved) {
		t.Fatalf("run over an unapproved week = %v, want PayrollNotApprovedError", err)
	}

	run, _, err := d.CreatePayrollDraft(ownerCtx, appmodel.PayrollDraftRequest{
		TeamID: teamID, CallerID: ownerID, Start: week1, End: week2, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("run over the approved week: %v", err)
	}
	rows, err := d.TestSQL().QueryContext(ctx,
		`SELECT user_id, seconds, rate_cents, amount_cents FROM payroll_lines WHERE run_id = ? ORDER BY user_id`, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[int64][4]int64{}
	for rows.Next() {
		var userID, seconds, rate, amount int64
		if err := rows.Scan(&userID, &seconds, &rate, &amount); err != nil {
			t.Fatal(err)
		}
		got[userID] = [4]int64{userID, seconds, rate, amount}
	}
	if got[ownerID] != [4]int64{ownerID, 3600, 4000, 4000} {
		t.Fatalf("owner line = %v, want 3600 s at 4000 = 4000 cents", got[ownerID])
	}
	if got[memberID] != [4]int64{memberID, 7200, 2500, 5000} {
		t.Fatalf("member line = %v, want 7200 s at 2500 = 5000 cents", got[memberID])
	}
}
