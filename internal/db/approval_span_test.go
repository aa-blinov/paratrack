package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// An approved day inside a week is still approved time. A row clear covers the
// whole week, so checking only the week's two ends let a one-day approval slip
// through and delete the approved sessions.
func TestApprovedDayInsideRowClearIsRefused(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)

	ownerID := seedApprovalUser(t, d, "span-owner@test.local")
	teamID, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: "span-team", Name: "Команда", OwnerID: ownerID})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	var activityID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO activities (name, team_id) VALUES ('Вёрстка', ?) RETURNING id`, teamID).Scan(&activityID); err != nil {
		t.Fatalf("activity: %v", err)
	}
	weekStart := time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC)
	wednesday := weekStart.AddDate(0, 0, 2)
	seedClosedSession(t, d, teamID, ownerID, activityID, wednesday.Add(10*time.Hour))

	ownerCtx := requestctx.WithActor(ctx, ownerID)
	if err := d.ApproveTimesheetWeek(ownerCtx, appmodel.TimesheetApprovalRequest{
		TeamID: teamID, CallerID: ownerID, Start: wednesday, End: wednesday.AddDate(0, 0, 1),
	}); err != nil {
		t.Fatalf("approve one day: %v", err)
	}

	err = d.ClearWeekRow(ownerCtx, appmodel.TimesheetRowClearRequest{
		TeamID: teamID, ActivityID: activityID, WeekStart: weekStart,
	})
	var lockErr *model.SessionApprovalLockError
	if !errors.As(err, &lockErr) {
		t.Fatalf("ClearWeekRow over an approved day = %v, want SessionApprovalLockError", err)
	}
	if count := rowClearSessionCount(t, d, activityID, ownerID); count != 1 {
		t.Fatalf("sessions after refused clear = %d, want the approved one kept", count)
	}
}
