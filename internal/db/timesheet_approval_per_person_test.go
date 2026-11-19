package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// A week approved for one member (TIME-23) closes that member's time and
// nobody else's: the other member keeps editing, the workspace-wide row is
// absent, and the payroll and invoice predicates count the other member's
// week as unapproved.
func TestTimesheetApprovalForOneMemberLocksOnlyThatMember(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)

	ownerID := seedApprovalUser(t, d, "per-person-owner@test.local")
	memberID := seedApprovalUser(t, d, "per-person-member@test.local")
	otherID := seedApprovalUser(t, d, "per-person-other@test.local")
	teamID, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: "per-person-team", Name: "Команда", OwnerID: ownerID})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	for _, member := range []int64{memberID, otherID} {
		if _, err := d.TestSQL().ExecContext(ctx,
			`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
			teamID, member, FormatTime(time.Now().UTC())); err != nil {
			t.Fatalf("join member: %v", err)
		}
	}
	var activityID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO activities (name, team_id) VALUES ('Правки', ?) RETURNING id`, teamID).Scan(&activityID); err != nil {
		t.Fatalf("activity: %v", err)
	}
	weekStart := time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC)
	weekEnd := weekStart.AddDate(0, 0, 7)
	memberSession := seedClosedSession(t, d, teamID, memberID, activityID, weekStart.Add(10*time.Hour))
	otherSession := seedClosedSession(t, d, teamID, otherID, activityID, weekStart.Add(11*time.Hour))
	memberCtx := requestctx.WithActor(ctx, memberID)
	otherCtx := requestctx.WithActor(ctx, otherID)

	// A week approved for a member the manager names.
	if err := d.ApproveTimesheetWeek(ctx, appmodel.TimesheetApprovalRequest{
		TeamID: teamID, CallerID: ownerID, Start: weekStart, End: weekEnd, UserID: memberID,
	}); err != nil {
		t.Fatalf("approve for one member: %v", err)
	}
	// Approving twice is not an error, exactly like the workspace-wide row.
	if err := d.ApproveTimesheetWeek(ctx, appmodel.TimesheetApprovalRequest{
		TeamID: teamID, CallerID: ownerID, Start: weekStart, End: weekEnd, UserID: memberID,
	}); err != nil {
		t.Fatalf("approve for one member again: %v", err)
	}
	// Approving for a stranger is refused.
	if err := d.ApproveTimesheetWeek(ctx, appmodel.TimesheetApprovalRequest{
		TeamID: teamID, CallerID: ownerID, Start: weekStart, End: weekEnd, UserID: 999999,
	}); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("approve for a stranger = %v, want ErrNotFound", err)
	}

	// The member's own time is closed for edits.
	if err := d.DeleteSession(memberCtx, appmodel.SessionDeleteRequest{TeamID: teamID, CallerID: memberID, SessionID: memberSession}); !isApprovalLock(err) {
		t.Fatalf("delete the member's session = %v, want SessionApprovalLockError", err)
	}
	// The other member's time in the same week stays open.
	if err := d.DeleteSession(otherCtx, appmodel.SessionDeleteRequest{TeamID: teamID, CallerID: otherID, SessionID: otherSession}); err != nil {
		t.Fatalf("delete the other member's session: %v", err)
	}
	if err := seedClosedSessionAgain(t, d, teamID, otherID, activityID, weekStart.Add(12*time.Hour)); err != nil {
		t.Fatalf("the other member adds time in the same week: %v", err)
	}

	// The payroll predicate counts the other member's time as unapproved, and
	// only it: the member's approved hours are not in the refusal.
	missing, err := d.UnapprovedPayrollTime(ctx, teamID, weekStart, weekEnd)
	if err != nil {
		t.Fatalf("unapproved payroll time: %v", err)
	}
	if missing == nil || missing.Count != 1 {
		t.Fatalf("unapproved payroll time = %+v, want exactly the other member's session", missing)
	}

	// Reopening removes exactly that member's row: a workspace-wide row of the
	// same week, if any, stays.
	if err := d.ReopenTimesheetWeek(ctx, appmodel.TimesheetApprovalRequest{
		TeamID: teamID, CallerID: ownerID, Start: weekStart, End: weekEnd, UserID: memberID,
	}); err != nil {
		t.Fatalf("reopen the member's week: %v", err)
	}
	// And the member's time is editable again.
	if err := d.DeleteSession(memberCtx, appmodel.SessionDeleteRequest{TeamID: teamID, CallerID: memberID, SessionID: memberSession}); err != nil {
		t.Fatalf("delete after reopen: %v", err)
	}
}

func seedClosedSessionAgain(t *testing.T, d *DB, teamID, userID, activityID int64, start time.Time) error {
	t.Helper()
	actor := requestctx.WithActor(t.Context(), userID)
	_, err := d.CreateClosedSession(actor, appmodel.TimerAddRequest{
		TeamID: teamID, ActivityID: activityID, Start: start, End: start.Add(time.Hour),
	})
	return err
}
