package db

import (
	"testing"
	"time"
)

// goalPeriodRange is the single source of truth for what window a
// goal's progress is measured against. Edge cases worth pinning:

func TestGoalPeriodRange_Daily(t *testing.T) {
	// Pick a non-midnight timestamp to verify we still land on day bounds.
	now := time.Date(2026, 9, 22, 15, 34, 12, 0, time.UTC)
	start, end := goalPeriodRange("daily", now)

	wantStart := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) {
		t.Errorf("daily start = %v, want %v", start, wantStart)
	}
	if !end.Equal(wantEnd) {
		t.Errorf("daily end = %v, want %v", end, wantEnd)
	}
}

func TestGoalPeriodRange_Daily_NewYear(t *testing.T) {
	now := time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)
	start, end := goalPeriodRange("daily", now)
	wantStart := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Errorf("Dec 31 daily: start=%v end=%v, want %v / %v", start, end, wantStart, wantEnd)
	}
}

func TestGoalPeriodRange_Weekly_MondayStart(t *testing.T) {
	// Wed 2026-09-23 should belong to the week starting Mon 2026-09-21.
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	start, end := goalPeriodRange("weekly", now)

	wantStart := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) {
		t.Errorf("weekly start = %v, want %v", start, wantStart)
	}
	if !end.Equal(wantEnd) {
		t.Errorf("weekly end = %v, want %v", end, wantEnd)
	}
}

func TestGoalPeriodRange_Weekly_Sunday(t *testing.T) {
	// Sunday 2026-09-27 is the last day of the ISO week that started Mon 09-21.
	now := time.Date(2026, 9, 27, 23, 30, 0, 0, time.UTC)
	start, end := goalPeriodRange("weekly", now)
	wantStart := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Errorf("Sunday weekly: start=%v end=%v, want %v / %v",
			start, end, wantStart, wantEnd)
	}
}

func TestGoalPeriodRange_Weekly_MondayExact(t *testing.T) {
	// Already Monday — should be in the week starting today, not last week.
	now := time.Date(2026, 9, 21, 0, 1, 0, 0, time.UTC)
	start, _ := goalPeriodRange("weekly", now)
	wantStart := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) {
		t.Errorf("Monday weekly: start=%v, want %v", start, wantStart)
	}
}

func TestGoalPeriodRange_Monthly(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	start, end := goalPeriodRange("monthly", now)
	wantStart := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Errorf("monthly: start=%v end=%v, want %v / %v", start, end, wantStart, wantEnd)
	}
}

func TestGoalPeriodRange_Monthly_YearRollover(t *testing.T) {
	now := time.Date(2026, 12, 15, 12, 0, 0, 0, time.UTC)
	start, end := goalPeriodRange("monthly", now)
	wantStart := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Errorf("Dec monthly: start=%v end=%v, want %v / %v",
			start, end, wantStart, wantEnd)
	}
}

// Sanity check that an unknown period falls back to a daily window —
// the defensive branch in goalPeriodRange should never be reached in
// production because UpsertGoal validates the value, but the fallback
// must still return *some* reasonable window.
func TestGoalPeriodRange_UnknownFallsBackToDaily(t *testing.T) {
	now := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	start, end := goalPeriodRange("nonsense", now)
	wantStart := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Errorf("unknown fallback: start=%v end=%v, want %v / %v",
			start, end, wantStart, wantEnd)
	}
}

func TestUpsertGoal_RejectsInvalidPeriod(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	act, err := d.GetOrCreateActivity(ctx, "test-up")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertGoal(ctx, act.ID, "yearly", 60); err == nil {
		t.Error("UpsertGoal with period=yearly should fail, got nil")
	}
	if _, err := d.UpsertGoal(ctx, act.ID, "daily", 0); err == nil {
		t.Error("UpsertGoal with 0 minutes should fail, got nil")
	}
	if _, err := d.UpsertGoal(ctx, act.ID, "daily", -10); err == nil {
		t.Error("UpsertGoal with negative minutes should fail, got nil")
	}
}

func TestUpsertGoal_ReplacesExistingForSamePeriod(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	act, err := d.GetOrCreateActivity(ctx, "test-replace")
	if err != nil {
		t.Fatal(err)
	}
	g1, err := d.UpsertGoal(ctx, act.ID, "daily", 60)
	if err != nil {
		t.Fatal(err)
	}
	g2, err := d.UpsertGoal(ctx, act.ID, "daily", 120)
	if err != nil {
		t.Fatal(err)
	}
	if g1.ID != g2.ID {
		t.Errorf("upsert created a second row: g1.ID=%d, g2.ID=%d", g1.ID, g2.ID)
	}
	if g2.TargetMinutes != 120 {
		t.Errorf("upsert didn't update target: got %d, want 120", g2.TargetMinutes)
	}
}

func TestDeleteGoal_MissingReturnsErrGoalNotFound(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	act, err := d.GetOrCreateActivity(ctx, "test-del")
	if err != nil {
		t.Fatal(err)
	}
	err = d.DeleteGoal(ctx, act.ID, "daily")
	if err != ErrGoalNotFound {
		t.Errorf("DeleteGoal on missing row: err=%v, want %v", err, ErrGoalNotFound)
	}
}

// openTestDB returns an isolated in-memory DB so the tests don't
// touch the user's real ~/.track/track.db. Uses modernc's
// ":memory:" DSN. Tables are created via the same Open() path the
// production code uses, so schema and migrations are exercised.
func openTestDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open :memory: db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}