package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// The clock is Saturday 24 October 2026 noon UTC, which is 15:00 in Moscow. With a
// 7-day term the first open day is 17 October, so 16 October 23:00 is closed and
// 17 October 09:00 is open.
var autoLockNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func autoLockWorkspace(t *testing.T, d *DB, slug string, days int, zone string) (int64, int64, int64) {
	t.Helper()
	teamID := seedTeam(t, d, "Autolock "+slug, slug)
	owner := teamOwner(t, d, teamID)
	ctx := requestctx.WithActor(t.Context(), owner)
	if err := d.SetTeamCalendar(ctx, appmodel.TeamCalendarRequest{
		TeamID: teamID, CallerID: owner, TimeZone: zone, WeekStart: "mon", AutoLockDays: days,
	}); err != nil {
		t.Fatalf("set the closing term to %d days: %v", days, err)
	}
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: owner, Name: "work"})
	if err != nil {
		t.Fatalf("create activity: %v", err)
	}
	return teamID, owner, activity.ID
}

func requireDateClosed(t *testing.T, err error, cutoff time.Time) {
	t.Helper()
	var lockErr *model.SessionAutoLockError
	if !errors.Is(err, model.ErrDateClosed) || !errors.As(err, &lockErr) {
		t.Fatalf("err = %v, want a refusal for the closing date", err)
	}
	if !lockErr.Before.Equal(cutoff) {
		t.Fatalf("refusal names %s as the first open day, want %s", lockErr.Before, cutoff)
	}
}

func TestAutoLockRefusesEveryWritePathThatStartsBeforeTheCutoff(t *testing.T) {
	d := openTestDB(t)
	d.now = func() time.Time { return autoLockNow }
	moscow, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	teamID, owner, activityID := autoLockWorkspace(t, d, "autolock-paths", 7, "Europe/Moscow")
	ctx := requestctx.WithActor(t.Context(), owner)
	cutoff := time.Date(2026, 10, 17, 0, 0, 0, 0, moscow)
	closedAt := time.Date(2026, 10, 16, 23, 0, 0, 0, moscow)
	openAt := time.Date(2026, 10, 17, 9, 0, 0, 0, moscow)

	if _, err := d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activityID, Start: closedAt, End: closedAt.Add(time.Hour)}); err == nil {
		t.Fatal("manual add at 16 October 23:00 was accepted, want refusal")
	} else {
		requireDateClosed(t, err, cutoff)
	}

	if _, err := d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activityID, Start: openAt, End: openAt.Add(time.Hour)}); err != nil {
		t.Fatalf("manual add on the first open day: %v, want nil", err)
	}

	if err := d.UpsertDayTotal(ctx, appmodel.TimesheetCellUpdateRequest{
		TeamID: teamID, ActivityID: activityID, Day: time.Date(2026, 10, 16, 0, 0, 0, 0, moscow), TotalSeconds: 3600,
	}); err == nil {
		t.Fatal("cell write on 16 October was accepted, want refusal")
	} else {
		requireDateClosed(t, err, cutoff)
	}

	if err := d.UpsertDayTotal(ctx, appmodel.TimesheetCellUpdateRequest{
		TeamID: teamID, ActivityID: activityID, Day: time.Date(2026, 10, 17, 0, 0, 0, 0, moscow), TotalSeconds: 3600,
	}); err != nil {
		t.Fatalf("cell write on 17 October: %v, want nil", err)
	}

	if err := d.ClearWeekRow(ctx, appmodel.TimesheetRowClearRequest{
		TeamID: teamID, ActivityID: activityID, WeekStart: time.Date(2026, 10, 12, 0, 0, 0, 0, moscow),
	}); err == nil {
		t.Fatal("row clear of the week of 12 October was accepted, want refusal")
	} else {
		requireDateClosed(t, err, cutoff)
	}

	if _, err := d.StartSession(ctx, appmodel.TimerStartRequest{TeamID: teamID, ActivityID: activityID, At: closedAt}); err == nil {
		t.Fatal("timer started at 16 October 23:00 was accepted, want refusal")
	} else {
		requireDateClosed(t, err, cutoff)
	}

	open, err := d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activityID, Start: openAt.Add(2 * time.Hour), End: openAt.Add(3 * time.Hour)})
	if err != nil {
		t.Fatalf("add a session to edit: %v", err)
	}
	moved := closedAt
	if err := d.UpdateSessionFields(ctx, appmodel.SessionUpdateRequest{
		TeamID: teamID, CallerID: owner, SessionID: open.ID,
		Update: appmodel.SessionUpdate{StartAt: &moved, UpdatedAt: autoLockNow},
	}); err == nil {
		t.Fatal("moving a session before the cutoff was accepted, want refusal")
	} else {
		requireDateClosed(t, err, cutoff)
	}

	if err := d.DeleteSession(ctx, appmodel.SessionDeleteRequest{TeamID: teamID, CallerID: owner, SessionID: open.ID}); err != nil {
		t.Fatalf("delete a session on an open day: %v, want nil", err)
	}
}

// A timer started before the closing date can be stopped, because a stop only
// writes the end. Its start stays put, so the edit, delete and reopen paths that
// would move or undo that start are refused.
func TestAutoLockLetsAForgottenTimerBeStoppedButNotChanged(t *testing.T) {
	d := openTestDB(t)
	d.now = func() time.Time { return autoLockNow }
	moscow, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	teamID, owner, activityID := autoLockWorkspace(t, d, "autolock-timer", 0, "Europe/Moscow")
	ctx := requestctx.WithActor(t.Context(), owner)
	cutoff := time.Date(2026, 10, 17, 0, 0, 0, 0, moscow)
	started := time.Date(2026, 10, 10, 9, 0, 0, 0, moscow)

	session, err := d.StartSession(ctx, appmodel.TimerStartRequest{TeamID: teamID, ActivityID: activityID, At: started})
	if err != nil {
		t.Fatalf("start the timer while the past is open: %v", err)
	}
	if err := d.SetTeamCalendar(ctx, appmodel.TeamCalendarRequest{TeamID: teamID, CallerID: owner, TimeZone: "Europe/Moscow", WeekStart: "mon", AutoLockDays: 7}); err != nil {
		t.Fatalf("turn the closing term on: %v", err)
	}

	if err := d.UpdateSessionFields(ctx, appmodel.SessionUpdateRequest{
		TeamID: teamID, CallerID: owner, SessionID: session.ID,
		Update: appmodel.SessionUpdate{Note: "edited", UpdatedAt: autoLockNow},
	}); err == nil {
		t.Fatal("note edit on a timer started before the cutoff was accepted, want refusal")
	} else {
		requireDateClosed(t, err, cutoff)
	}

	stopped, err := d.UpdateSessionEnd(ctx, appmodel.TimerStopRequest{TeamID: teamID, SessionID: session.ID, At: autoLockNow})
	if err != nil {
		t.Fatalf("stop the forgotten timer: %v, want nil", err)
	}
	if !stopped.StartAt.Equal(started) {
		t.Fatalf("stop moved the start to %s, want %s", stopped.StartAt, started)
	}

	if err := d.DeleteSession(ctx, appmodel.SessionDeleteRequest{TeamID: teamID, CallerID: owner, SessionID: session.ID}); err == nil {
		t.Fatal("delete of a stopped session before the cutoff was accepted, want refusal")
	} else {
		requireDateClosed(t, err, cutoff)
	}

	if _, err := d.ReopenSession(ctx, appmodel.TimerReopenRequest{
		TeamID: teamID, SessionID: session.ID, At: autoLockNow, ExpectedEndAt: *stopped.EndAt,
	}); err == nil {
		t.Fatal("reopen of a session before the cutoff was accepted, want refusal")
	} else {
		requireDateClosed(t, err, cutoff)
	}
}

// With the term off nothing is refused, and the cutoff is reported as absent. A
// blank zone counts the day in UTC, the same fallback the workspace calendar uses.
func TestAutoLockTermOffAcceptsThePastAndBlankZoneCountsInUTC(t *testing.T) {
	d := openTestDB(t)
	d.now = func() time.Time { return autoLockNow }
	teamID, owner, activityID := autoLockWorkspace(t, d, "autolock-off", 0, "")
	ctx := requestctx.WithActor(t.Context(), owner)

	if _, on, err := d.TimesheetAutoLockCutoff(ctx, teamID); err != nil || on {
		t.Fatalf("cutoff with the term off: on = %v, err = %v, want off", on, err)
	}

	past := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	if _, err := d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activityID, Start: past, End: past.Add(time.Hour)}); err != nil {
		t.Fatalf("add a past session with the term off: %v, want nil", err)
	}

	if err := d.SetTeamCalendar(ctx, appmodel.TeamCalendarRequest{TeamID: teamID, CallerID: owner, WeekStart: "mon", AutoLockDays: 7}); err != nil {
		t.Fatalf("turn the term on with a blank zone: %v", err)
	}
	cutoff, on, err := d.TimesheetAutoLockCutoff(ctx, teamID)
	if err != nil {
		t.Fatal(err)
	}
	wantCutoff := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	if !on || !cutoff.Equal(wantCutoff) {
		t.Fatalf("cutoff = %s (on %v), want %s on", cutoff, on, wantCutoff)
	}
	late := time.Date(2026, 10, 16, 23, 30, 0, 0, time.UTC)
	if _, err := d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activityID, Start: late, End: late.Add(time.Hour)}); err == nil {
		t.Fatal("session at 16 October 23:30 UTC was accepted with a UTC cutoff of 17 October")
	}
}

// The cutoff is the start of the local day that was N days ago, and time.Date
// carries the day across a month boundary, so 7 days before 3 March is 24 February.
func TestAutoLockCutoffCrossesAMonthBoundary(t *testing.T) {
	now := time.Date(2026, 3, 3, 0, 30, 0, 0, time.UTC)
	got := autoLockCutoff(now, 7, time.UTC)
	want := time.Date(2026, 2, 24, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("cutoff = %s, want %s", got, want)
	}
}
