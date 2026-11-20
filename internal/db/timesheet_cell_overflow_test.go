package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// A cell above fifteen hours must stay inside its day. The sheet session
// anchored at 09:00 would cross midnight with the surplus, and the grid
// files a session under the day it starts on, so the hours would surface on
// the next day's cell and, on the week's last day, in the next week.
func TestTimesheetCellAboveFifteenHoursStaysInTheDay(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "cell-overflow", "cell-overflow")
	ownerID := teamOwner(t, d, teamID)
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "cell overflow"})
	if err != nil {
		t.Fatal(err)
	}
	ownerCtx := requestctx.WithActor(ctx, ownerID)
	day := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	nextDay := day.AddDate(0, 0, 1)

	if err := d.UpsertDayTotal(ownerCtx, appmodel.TimesheetCellUpdateRequest{
		TeamID: teamID, ActivityID: activity.ID, Day: day, TotalSeconds: 16 * 3600,
	}); err != nil {
		t.Fatalf("set a 16h cell: %v", err)
	}
	var startRaw, endRaw string
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT start_at, end_at FROM sessions WHERE activity_id = ? AND team_id = ? AND user_id = ? AND note = 'timesheet'`,
		activity.ID, teamID, ownerID).Scan(&startRaw, &endRaw); err != nil {
		t.Fatalf("read the sheet session: %v", err)
	}
	startAt, err := ParseTime(startRaw)
	if err != nil {
		t.Fatal(err)
	}
	endAt, err := ParseTime(endRaw)
	if err != nil {
		t.Fatal(err)
	}
	if startAt.Before(day) || endAt.After(nextDay) {
		t.Fatalf("the sheet session %s..%s leaves its day %s..%s", startRaw, endRaw, FormatTime(day), FormatTime(nextDay))
	}
	if got := endAt.Sub(startAt); got != 16*time.Hour {
		t.Fatalf("the sheet session holds %s, want the whole 16h", got)
	}
}
