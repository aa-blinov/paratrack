package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// A manager approves a finished week; the approved week is read-only for the
// workspace until the approval is removed, a pay run counts only approved time,
// and a week a pay run covers cannot be reopened.
func TestTimesheetApprovalLocksWeekAndGatesPayroll(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)

	ownerID := seedApprovalUser(t, d, "approval-owner@test.local")
	memberID := seedApprovalUser(t, d, "approval-member@test.local")
	teamID, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: "approval-team", Name: "Команда", OwnerID: ownerID})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, memberID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatalf("join member: %v", err)
	}
	var activityID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO activities (name, team_id) VALUES ('Вёрстка', ?) RETURNING id`, teamID).Scan(&activityID); err != nil {
		t.Fatalf("activity: %v", err)
	}
	// Two finished weeks in the past, and one closed session in each.
	weekStart := time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC)
	weekEnd := weekStart.AddDate(0, 0, 7)
	nextStart := weekEnd
	nextEnd := nextStart.AddDate(0, 0, 7)
	inWeek := seedClosedSession(t, d, teamID, ownerID, activityID, weekStart.Add(10*time.Hour))
	inNext := seedClosedSession(t, d, teamID, ownerID, activityID, nextStart.Add(10*time.Hour))

	approve := func(caller int64, start, end time.Time) error {
		return d.ApproveTimesheetWeek(ctx, appmodel.TimesheetApprovalRequest{TeamID: teamID, CallerID: caller, Start: start, End: end})
	}

	// A member cannot approve, and an unfinished week is refused.
	if err := approve(memberID, weekStart, weekEnd); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("member approve = %v, want ErrForbidden", err)
	}
	future := time.Now().UTC().AddDate(0, 1, 0)
	if err := approve(ownerID, future, future.AddDate(0, 0, 7)); !errors.Is(err, model.ErrWeekNotFinished) {
		t.Fatalf("approve an unfinished week = %v, want ErrWeekNotFinished", err)
	}

	// The manager approves the first week; approving it again changes nothing.
	if err := approve(ownerID, weekStart, weekEnd); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := approve(ownerID, weekStart, weekEnd); err != nil {
		t.Fatalf("approve again: %v", err)
	}

	// Time that starts in the approved week cannot be deleted or added there.
	if err := d.DeleteSession(ctx, appmodel.SessionDeleteRequest{TeamID: teamID, CallerID: ownerID, SessionID: inWeek}); !isApprovalLock(err) {
		t.Fatalf("delete in an approved week = %v, want SessionApprovalLockError", err)
	}
	actor := requestctx.WithTeamID(requestctx.WithActor(ctx, ownerID), teamID)
	if _, err := d.CreateClosedSession(actor, appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activityID, Start: weekStart.Add(20 * time.Hour), End: weekStart.Add(21 * time.Hour)}); !isApprovalLock(err) {
		t.Fatalf("add in an approved week = %v, want SessionApprovalLockError", err)
	}

	// Pay runs count only approved time: the second week is not approved yet.
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := requireApprovedPayrollTime(ctx, tx, teamID, weekStart, nextEnd); err == nil {
		t.Fatal("payroll over an unapproved week was allowed")
	} else {
		var notApproved *model.PayrollNotApprovedError
		if !errors.As(err, &notApproved) || notApproved.Count != 1 {
			t.Fatalf("payroll refusal = %v, want one unapproved session", err)
		}
	}
	if err := requireApprovedPayrollTime(ctx, tx, teamID, weekStart, weekEnd); err != nil {
		t.Fatalf("payroll over the approved week: %v", err)
	}
	_ = inNext

	// A pay run covers the week, so the approval cannot be removed.
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO payroll_runs (team_id, number, period_start, period_end, status, notes, currency, created_at)
		 VALUES (?, 'PAY-TEST-1', ?, ?, 'paid', '', 'RUB', ?)`,
		teamID, FormatTime(weekStart), FormatTime(weekEnd), FormatTime(time.Now().UTC())); err != nil {
		t.Fatalf("seed pay run: %v", err)
	}
	reopen := func(caller int64) error {
		return d.ReopenTimesheetWeek(ctx, appmodel.TimesheetApprovalRequest{TeamID: teamID, CallerID: caller, Start: weekStart, End: weekEnd})
	}
	if err := reopen(ownerID); !errors.Is(err, model.ErrWeekPaid) {
		t.Fatalf("reopen a paid week = %v, want ErrWeekPaid", err)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `DELETE FROM payroll_runs WHERE team_id = ? AND number = 'PAY-TEST-1'`, teamID); err != nil {
		t.Fatalf("remove pay run: %v", err)
	}

	// Once reopened, the week is editable again.
	if err := reopen(memberID); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("member reopen = %v, want ErrForbidden", err)
	}
	if err := reopen(ownerID); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if err := d.DeleteSession(ctx, appmodel.SessionDeleteRequest{TeamID: teamID, CallerID: ownerID, SessionID: inWeek}); err != nil {
		t.Fatalf("delete after reopen: %v", err)
	}
}

func isApprovalLock(err error) bool {
	var lock *model.SessionApprovalLockError
	return errors.As(err, &lock)
}

func seedApprovalUser(t *testing.T, d *DB, email string) int64 {
	t.Helper()
	var id int64
	if err := d.TestSQL().QueryRowContext(t.Context(),
		`INSERT INTO users (email, password_hash, name) VALUES (?, 'x', ?) RETURNING id`, email, email).Scan(&id); err != nil {
		t.Fatalf("seed user %s: %v", email, err)
	}
	return id
}

func seedClosedSession(t *testing.T, d *DB, teamID, userID, activityID int64, start time.Time) int64 {
	t.Helper()
	var id int64
	if err := d.TestSQL().QueryRowContext(t.Context(),
		`INSERT INTO sessions (activity_id, team_id, user_id, start_at, end_at, accumulated_seconds)
		 VALUES (?, ?, ?, ?, ?, 3600) RETURNING id`,
		activityID, teamID, userID, FormatTime(start), FormatTime(start.Add(time.Hour))).Scan(&id); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return id
}
