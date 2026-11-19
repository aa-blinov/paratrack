package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// The clock is Saturday 24 October 2026 noon UTC: September 2026 is over,
// October is not, so a close of September stands and a close of October
// refuses.
func closedPeriodClock(t *testing.T, d *DB) {
	t.Helper()
	d.now = func() time.Time { return time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC) }
}

func closedPeriodWorkspace(t *testing.T, d *DB, slug string) (int64, int64, int64, *time.Location) {
	t.Helper()
	teamID := seedTeam(t, d, "Закрытие "+slug, slug)
	owner := teamOwner(t, d, teamID)
	ctx := requestctx.WithActor(t.Context(), owner)
	if err := d.SetTeamCalendar(ctx, appmodel.TeamCalendarRequest{
		TeamID: teamID, CallerID: owner, TimeZone: "Europe/Moscow", WeekStart: "mon",
	}); err != nil {
		t.Fatalf("set the workspace zone: %v", err)
	}
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: owner, Name: "work"})
	if err != nil {
		t.Fatalf("create activity: %v", err)
	}
	moscow, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	return teamID, owner, activity.ID, moscow
}

// Closing the month fixes what it holds: a session inside it refuses every
// write path the approved-week and autolock guards sit on, and the refusal
// names the month the way it reads in the workspace's own zone.
func TestCloseMonthFixesItsTime(t *testing.T) {
	d := openTestDB(t)
	closedPeriodClock(t, d)
	teamID, owner, activityID, moscow := closedPeriodWorkspace(t, d, "close-time")
	ctx := requestctx.WithActor(t.Context(), owner)
	septDay := time.Date(2026, 9, 14, 9, 0, 0, 0, moscow)
	kept, err := d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activityID, Start: septDay, End: septDay.Add(time.Hour)})
	if err != nil {
		t.Fatalf("seed a September hour: %v", err)
	}
	if err := d.CloseMonth(ctx, appmodel.PeriodCloseRequest{TeamID: teamID, CallerID: owner, Month: "2026-10-01"}); !errors.Is(err, model.ErrMonthNotFinished) {
		t.Fatalf("closing the running October = %v, want a refusal for an unfinished month", err)
	}
	if err := d.CloseMonth(ctx, appmodel.PeriodCloseRequest{TeamID: teamID, CallerID: owner, Month: "2026-09-01"}); err != nil {
		t.Fatalf("close September: %v", err)
	}
	// Closing twice stands on the first close.
	if err := d.CloseMonth(ctx, appmodel.PeriodCloseRequest{TeamID: teamID, CallerID: owner, Month: "2026-09-01"}); err != nil {
		t.Fatalf("closing September twice: %v", err)
	}

	var closedErr *model.PeriodClosedError
	if _, err := d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activityID, Start: septDay.Add(2 * time.Hour), End: septDay.Add(3 * time.Hour)}); !errors.As(err, &closedErr) {
		t.Fatalf("a new September hour after the close = %v, want a period refusal", err)
	} else if closedErr.Month.Format("2006-01-02") != "2026-09-01" {
		t.Fatalf("the refusal names %s, want the month's first day", closedErr.Month)
	}
	if _, err := d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activityID, Start: time.Date(2026, 10, 12, 9, 0, 0, 0, moscow), End: time.Date(2026, 10, 12, 10, 0, 0, 0, moscow)}); err != nil {
		t.Fatalf("an October hour stays writable after September closed: %v", err)
	}
	if err := d.DeleteSession(ctx, appmodel.SessionDeleteRequest{TeamID: teamID, SessionID: kept.ID, CallerID: owner}); !errors.As(err, &closedErr) {
		t.Fatalf("deleting a September session after the close = %v, want a period refusal", err)
	}

	// Reopening lifts the gate, and the same writes go through again.
	if err := d.ReopenMonth(ctx, appmodel.PeriodCloseRequest{TeamID: teamID, CallerID: owner, Month: "2026-09-01"}); err != nil {
		t.Fatalf("reopen September: %v", err)
	}
	if _, err := d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activityID, Start: septDay.Add(2 * time.Hour), End: septDay.Add(3 * time.Hour)}); err != nil {
		t.Fatalf("a September hour after the reopen: %v", err)
	}
}

// A month with a timer nobody stopped cannot close: closing would strand the
// timer inside a read-only month, so the close names it instead.
func TestCloseMonthRefusesRunningTimersInside(t *testing.T) {
	d := openTestDB(t)
	closedPeriodClock(t, d)
	teamID, owner, activityID, moscow := closedPeriodWorkspace(t, d, "close-running")
	ctx := requestctx.WithActor(t.Context(), owner)
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO sessions (activity_id, team_id, user_id, start_at, accumulated_seconds, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 0, ?, ?)`,
		activityID, teamID, owner, FormatTime(time.Date(2026, 9, 14, 9, 0, 0, 0, moscow)),
		FormatTime(time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)), FormatTime(time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC))); err != nil {
		t.Fatalf("seed the forgotten timer: %v", err)
	}
	if err := d.CloseMonth(ctx, appmodel.PeriodCloseRequest{TeamID: teamID, CallerID: owner, Month: "2026-09-01"}); !errors.Is(err, model.ErrWeekHasRunningTimer) {
		t.Fatalf("closing with a running timer = %v, want a refusal that names the timer", err)
	}
	if closed, _ := d.ListClosedMonths(ctx, teamID); len(closed) != 0 {
		t.Fatalf("the refused close left %d rows behind", len(closed))
	}
}

// The span guard behind the invoice and pay-run writes refuses a document that
// overlaps a closed month and lets one in the closed months' shadow through:
// one check serves both bookkeeping paths.
func TestClosedPeriodSpanGuardsTheBooks(t *testing.T) {
	d := openTestDB(t)
	closedPeriodClock(t, d)
	teamID, owner, _, moscow := closedPeriodWorkspace(t, d, "close-books")
	ctx := requestctx.WithActor(t.Context(), owner)
	if err := d.CloseMonth(ctx, appmodel.PeriodCloseRequest{TeamID: teamID, CallerID: owner, Month: "2026-09-01"}); err != nil {
		t.Fatalf("close September: %v", err)
	}
	span := func(from, to time.Time) error {
		tx, err := d.sql.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		return lockClosedPeriodSpan(ctx, tx, teamID, from, to)
	}
	if err := span(time.Date(2026, 8, 25, 0, 0, 0, 0, moscow), time.Date(2026, 9, 25, 0, 0, 0, 0, moscow)); err == nil {
		t.Fatal("a document over September was accepted, want a refusal")
	}
	if err := span(time.Date(2026, 10, 1, 0, 0, 0, 0, moscow), time.Date(2026, 10, 20, 0, 0, 0, 0, moscow)); err != nil {
		t.Fatalf("a document over the open October = %v, want a pass", err)
	}
	// A span that ends at midnight into the open October still holds a closed
	// September day: its last instant is inside September.
	if err := span(time.Date(2026, 9, 25, 0, 0, 0, 0, moscow), time.Date(2026, 10, 1, 0, 0, 0, 0, moscow)); err == nil {
		t.Fatal("a span ending at the month boundary passed, want the September day named")
	}
}

// A member cannot close or reopen a month; the whole-month gate is a
// bookkeeping decision, and the store keeps it with the manager role.
func TestCloseMonthNeedsAManager(t *testing.T) {
	d := openTestDB(t)
	closedPeriodClock(t, d)
	teamID, owner, _, _ := closedPeriodWorkspace(t, d, "close-role")
	memberID := seedProjectTestUser(t, d, "close-member")
	addMember(t, d, teamID, memberID, "member")
	if err := d.CloseMonth(requestctx.WithActor(t.Context(), memberID), appmodel.PeriodCloseRequest{TeamID: teamID, CallerID: memberID, Month: "2026-09-01"}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("a member's close = %v, want forbidden", err)
	}
	if err := d.ReopenMonth(requestctx.WithActor(t.Context(), memberID), appmodel.PeriodCloseRequest{TeamID: teamID, CallerID: memberID, Month: "2026-09-01"}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("a member's reopen = %v, want forbidden", err)
	}
	// The owner's close leaves one listed row with the closer's id on it.
	if err := d.CloseMonth(requestctx.WithActor(t.Context(), owner), appmodel.PeriodCloseRequest{TeamID: teamID, CallerID: owner, Month: "2026-09-01"}); err != nil {
		t.Fatalf("the owner's close: %v", err)
	}
	closed, err := d.ListClosedMonths(t.Context(), teamID)
	if err != nil {
		t.Fatal(err)
	}
	if len(closed) != 1 || closed[0].ClosedByID != owner || closed[0].Month.Format("2006-01-02") != "2026-09-01" {
		t.Fatalf("closed = %+v, want one row for 2026-09-01 closed by the owner", closed)
	}
}
