package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// rowClearWeek is the Monday the row-clear tests fill: cells written on
// Tuesday, a tracked session on Thursday, and a session in the next week that
// must survive the clear.
func rowClearWeek() time.Time { return time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC) }

func rowClearFixture(t *testing.T, d *DB, slug string) (int64, int64, model.Activity, time.Time) {
	t.Helper()
	teamID := seedTeam(t, d, slug, slug)
	ownerID := teamOwner(t, d, teamID)
	activity, err := d.GetOrCreateActivityForMember(t.Context(),
		appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "row clear"})
	if err != nil {
		t.Fatal(err)
	}
	return teamID, ownerID, activity, rowClearWeek()
}

func rowClearSessionCount(t *testing.T, d *DB, activityID, userID int64) int {
	t.Helper()
	var count int
	if err := d.TestSQL().QueryRowContext(t.Context(),
		`SELECT count(*) FROM sessions WHERE activity_id = ? AND user_id = ?`, activityID, userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestClearWeekRowRemovesEveryCellOfTheWeekForTheActor(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID, ownerID, activity, weekStart := rowClearFixture(t, d, "row-clear-week")
	ownerCtx := requestctx.WithActor(ctx, ownerID)

	// A hand-written cell becomes one synthetic sheet session, a tracked
	// session sits next to it, and a third session belongs to the next week.
	if err := d.UpsertDayTotal(ownerCtx, appmodel.TimesheetCellUpdateRequest{
		TeamID: teamID, ActivityID: activity.ID, Day: weekStart.AddDate(0, 0, 1), TotalSeconds: 5400,
	}); err != nil {
		t.Fatalf("write cell: %v", err)
	}
	tracked := weekStart.AddDate(0, 0, 3).Add(10 * time.Hour)
	if _, err := d.CreateClosedSession(ownerCtx, appmodel.TimerAddRequest{
		TeamID: teamID, ActivityID: activity.ID, Start: tracked, End: tracked.Add(time.Hour), Note: "tracked",
	}); err != nil {
		t.Fatalf("create tracked session: %v", err)
	}
	nextWeek := weekStart.AddDate(0, 0, 7).Add(10 * time.Hour)
	if _, err := d.CreateClosedSession(ownerCtx, appmodel.TimerAddRequest{
		TeamID: teamID, ActivityID: activity.ID, Start: nextWeek, End: nextWeek.Add(time.Hour), Note: "next week",
	}); err != nil {
		t.Fatalf("create next week session: %v", err)
	}
	grid, err := d.ListTimesheet(ownerCtx, appmodel.TimesheetRequest{
		TeamID: teamID, WeekStart: weekStart, Now: weekStart.AddDate(0, 0, 6).Add(12 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(grid.Rows) != 1 || grid.Rows[0].RowTotal != 9000 {
		t.Fatalf("grid before clear = %+v, want one row with 9000 seconds", grid.Rows)
	}

	if err := d.ClearWeekRow(ownerCtx, appmodel.TimesheetRowClearRequest{
		TeamID: teamID, ActivityID: activity.ID, WeekStart: weekStart,
	}); err != nil {
		t.Fatalf("ClearWeekRow: %v", err)
	}
	grid, err = d.ListTimesheet(ownerCtx, appmodel.TimesheetRequest{
		TeamID: teamID, WeekStart: weekStart, Now: weekStart.AddDate(0, 0, 6).Add(12 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(grid.Rows) != 1 || grid.Rows[0].RowTotal != 0 || grid.Rows[0].Secs != [7]int{} {
		t.Fatalf("grid after clear = %+v, want one empty row", grid.Rows)
	}
	if grid.GrandTotal != 0 {
		t.Fatalf("week total after clear = %d, want 0", grid.GrandTotal)
	}
	if count := rowClearSessionCount(t, d, activity.ID, ownerID); count != 1 {
		t.Fatalf("owner sessions after clear = %d, want only the next week one", count)
	}
}

func TestClearWeekRowKeepsAColleaguesTimeInTheSameWeek(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID, ownerID, activity, weekStart := rowClearFixture(t, d, "row-clear-colleague")
	memberID := seedProjectTestUser(t, d, "row-clear-member")
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, memberID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	memberStart := weekStart.AddDate(0, 0, 2).Add(9 * time.Hour)
	if _, err := d.CreateClosedSession(requestctx.WithActor(ctx, memberID), appmodel.TimerAddRequest{
		TeamID: teamID, ActivityID: activity.ID, Start: memberStart, End: memberStart.Add(time.Hour), Note: "colleague",
	}); err != nil {
		t.Fatalf("create colleague session: %v", err)
	}

	if err := d.ClearWeekRow(requestctx.WithActor(ctx, ownerID), appmodel.TimesheetRowClearRequest{
		TeamID: teamID, ActivityID: activity.ID, WeekStart: weekStart,
	}); err != nil {
		t.Fatalf("ClearWeekRow: %v", err)
	}
	if count := rowClearSessionCount(t, d, activity.ID, memberID); count != 1 {
		t.Fatalf("colleague sessions after owner clear = %d, want 1", count)
	}
}

func TestClearWeekRowRequiresCurrentWorkspaceMembership(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID, ownerID, activity, weekStart := rowClearFixture(t, d, "row-clear-membership")
	memberID := seedProjectTestUser(t, d, "row-clear-removed")
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, memberID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	start := weekStart.AddDate(0, 0, 4).Add(11 * time.Hour)
	if _, err := d.CreateClosedSession(requestctx.WithActor(ctx, memberID), appmodel.TimerAddRequest{
		TeamID: teamID, ActivityID: activity.ID, Start: start, End: start.Add(time.Hour), Note: "before removal",
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	clear := func(actorCtx context.Context) error {
		return d.ClearWeekRow(actorCtx, appmodel.TimesheetRowClearRequest{
			TeamID: teamID, ActivityID: activity.ID, WeekStart: weekStart,
		})
	}

	// A request without an authenticated actor never reaches the grid.
	if err := clear(ctx); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("clear without actor error = %v, want forbidden", err)
	}
	if err := d.RemoveTeamMember(ctx, appmodel.TeamMemberRemovalRequest{TeamID: teamID, TargetUserID: memberID, CallerID: ownerID, LeftAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	// A removed member misses the same membership guard a cell write misses.
	if err := clear(requestctx.WithActor(ctx, memberID)); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("removed member clear error = %v, want not found", err)
	}
	if count := rowClearSessionCount(t, d, activity.ID, memberID); count != 1 {
		t.Fatalf("refused clears left %d of 1 sessions", count)
	}
}

func TestClearWeekRowRefusesTimeBilledOnSentInvoice(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID, ownerID, activity, weekStart := rowClearFixture(t, d, "row-clear-invoice")
	ownerCtx := requestctx.WithActor(ctx, ownerID)
	start := weekStart.AddDate(0, 0, 3).Add(14 * time.Hour)
	session, err := d.CreateClosedSession(ownerCtx, appmodel.TimerAddRequest{
		TeamID: teamID, ActivityID: activity.ID, Start: start, End: start.Add(2 * time.Hour), Note: "billed",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	weekEnd := weekStart.AddDate(0, 0, 6)
	var invoiceID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO invoices (team_id, number, client_name, period_start, period_end, status, created_at)
		 VALUES (?, 'INV-ROW-CLEAR', 'Client', ?, ?, 'sent', ?) RETURNING id`,
		teamID, FormatTime(weekStart), FormatTime(weekEnd), FormatTime(time.Now().UTC())).Scan(&invoiceID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE sessions SET invoice_id = ? WHERE id = ?`, invoiceID, session.ID); err != nil {
		t.Fatal(err)
	}

	err = d.ClearWeekRow(ownerCtx, appmodel.TimesheetRowClearRequest{TeamID: teamID, ActivityID: activity.ID, WeekStart: weekStart})
	var lockErr *model.SessionInvoiceLockError
	if !errors.As(err, &lockErr) {
		t.Fatalf("clear of billed week error = %v, want invoice lock", err)
	}
	if lockErr.InvoiceNumber != "INV-ROW-CLEAR" {
		t.Fatalf("invoice number = %q, want INV-ROW-CLEAR", lockErr.InvoiceNumber)
	}
	if count := rowClearSessionCount(t, d, activity.ID, ownerID); count != 1 {
		t.Fatalf("billed session count after refusal = %d, want 1", count)
	}
}
