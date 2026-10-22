package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// The plan and the tracked time are split by week, and the weekly norm is kept
// per project (PLAN-02).
func TestProjectWeekTotalsSplitPlanAndTrackedByWeek(t *testing.T) {
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Недели проекта", "project-weeks")
	ownerID := teamOwner(t, d, teamID)
	ctx := requestctx.WithActor(t.Context(), ownerID)
	proj, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Сайт", Color: "#7c3aed"})
	if err != nil {
		t.Fatal(err)
	}
	activityID := newTestActivity(t, d, ctx, teamID, ownerID, "вёрстка")
	if err := d.AssignActivityProject(ctx, appmodel.AssignActivityProjectRequest{TeamID: teamID, ActivityID: activityID, ProjectID: proj.ID, CallerID: ownerID}); err != nil {
		t.Fatal(err)
	}
	plan := func(day time.Time, minutes int) {
		t.Helper()
		if err := d.UpsertScheduleEntry(ctx, appmodel.ScheduleCellRequest{TeamID: teamID, ActorID: ownerID, UserID: ownerID, ProjectID: proj.ID, Day: day, Minutes: minutes}); err != nil {
			t.Fatal(err)
		}
	}
	monday := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	plan(monday, 600)
	plan(monday.AddDate(0, 0, 2), 600)
	plan(monday.AddDate(0, 0, 8), 300)
	start := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	if _, err := d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activityID, Start: start, End: start.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	weeks := []time.Time{monday.AddDate(0, 0, -7), monday, monday.AddDate(0, 0, 7)}
	totals, err := d.ListProjectWeekTotals(ctx, teamID, proj.ID, weeks, time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	wantPlan := []int{0, 1200, 300}
	wantTracked := []int{0, 3600, 0}
	for i := range weeks {
		if totals[i].PlannedMinutes != wantPlan[i] || totals[i].TrackedSeconds != wantTracked[i] {
			t.Errorf("week %d: planned %d min, tracked %d s; want %d and %d", i, totals[i].PlannedMinutes, totals[i].TrackedSeconds, wantPlan[i], wantTracked[i])
		}
	}

	if err := d.SetProjectWeeklyNorm(ctx, appmodel.ProjectWeeklyNormRequest{TeamID: teamID, ProjectID: proj.ID, CallerID: ownerID, Minutes: 1800}); err != nil {
		t.Fatal(err)
	}
	if minutes, ok, err := d.GetProjectWeeklyNorm(ctx, teamID, proj.ID); err != nil || !ok || minutes != 1800 {
		t.Fatalf("norm after set = %d, %v, %v; want 1800", minutes, ok, err)
	}
	if err := d.SetProjectWeeklyNorm(ctx, appmodel.ProjectWeeklyNormRequest{TeamID: teamID, ProjectID: proj.ID, CallerID: ownerID, Minutes: 0}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := d.GetProjectWeeklyNorm(ctx, teamID, proj.ID); err != nil || ok {
		t.Fatalf("norm after a zero set: ok %v, err %v; want no norm", ok, err)
	}
}
