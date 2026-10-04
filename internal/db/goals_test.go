package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestProgressForGoalsBatchesWorkspaceActivitySessions(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Goal progress", "goal-progress")
	ownerID := teamOwner(t, d, teamID)
	ctx = requestctx.WithActor(ctx, ownerID)
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "focus"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	for _, period := range []string{"daily", "weekly", "monthly"} {
		if _, err := d.UpsertGoalForManager(ctx, appmodel.GoalUpsertRequest{TeamID: teamID, CallerID: ownerID, ActivityName: activity.Name, Period: period, Minutes: 120}); err != nil {
			t.Fatalf("create %s goal: %v", period, err)
		}
	}
	if _, err := d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activity.ID, Start: now.Add(-30 * time.Minute), End: now, Note: "focus"}); err != nil {
		t.Fatalf("create tracked session: %v", err)
	}

	progress, err := d.ProgressForGoals(ctx, teamID, now)
	if err != nil {
		t.Fatalf("ProgressForGoals: %v", err)
	}
	if len(progress) != 3 {
		t.Fatalf("progress count = %d, want 3", len(progress))
	}
	for _, item := range progress {
		if item.ActivityName != "focus" || item.AchievedMinutes != 30 || item.PercentComplete != 25 {
			t.Errorf("%s progress = %+v, want focus with 30 minutes and 25%%", item.Goal.Period, item)
		}
	}
}

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
	teamID := seedTeam(t, d, "Goal validation", "goal-validation")
	ownerID := teamOwner(t, d, teamID)
	request := appmodel.GoalUpsertRequest{TeamID: teamID, CallerID: ownerID, ActivityName: "test-up", Period: "yearly", Minutes: 60}
	if _, err := d.UpsertGoalForManager(ctx, request); err == nil {
		t.Error("UpsertGoal with period=yearly should fail, got nil")
	}
	request.Period, request.Minutes = "daily", 0
	if _, err := d.UpsertGoalForManager(ctx, request); err == nil {
		t.Error("UpsertGoal with 0 minutes should fail, got nil")
	}
	request.Minutes = -10
	if _, err := d.UpsertGoalForManager(ctx, request); err == nil {
		t.Error("UpsertGoal with negative minutes should fail, got nil")
	}
}

func TestUpsertGoal_ReplacesExistingForSamePeriod(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Goal replacement", "goal-replacement")
	ownerID := teamOwner(t, d, teamID)
	request := appmodel.GoalUpsertRequest{TeamID: teamID, CallerID: ownerID, ActivityName: "test-replace", Period: "daily", Minutes: 60}
	g1, err := d.UpsertGoalForManager(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	request.Minutes = 120
	g2, err := d.UpsertGoalForManager(ctx, request)
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

func TestDeleteGoalForManager_MissingGoalReturnsErrGoalNotFound(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Goal deletion", "goal-deletion")
	ownerID := teamOwner(t, d, teamID)
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "focus"})
	if err != nil {
		t.Fatal(err)
	}
	err = d.DeleteGoalForManager(ctx, appmodel.GoalDeleteRequest{
		TeamID: teamID, CallerID: ownerID, ActivityName: activity.Name, Period: "daily",
	})
	if !errors.Is(err, model.ErrGoalNotFound) {
		t.Fatalf("DeleteGoalForManager error = %v, want %v", err, model.ErrGoalNotFound)
	}
}

// openTestDB returns a dedicated Postgres schema for this test. The
// schema is dropped during cleanup; opening the DB applies the same
// schema and migrations used by production.
func openTestDB(t *testing.T) *DB {
	t.Helper()
	d, err := OpenTest(t)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}
