package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// The team's fact per project counts each session on the project it sits on, over the days
// asked for (PLAN-05): an hour on one project and two hours on another.
func TestTrackedSecondsByProjectSplitsTheFactPerProject(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Обзор", "team-overview")
	ownerID := teamOwner(t, d, teamID)
	first, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Первый", Color: "#7c3aed"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Второй", Color: "#7c3aed"})
	if err != nil {
		t.Fatal(err)
	}
	firstActivity := newTestActivity(t, d, ctx, teamID, ownerID, "Первая работа")
	secondActivity := newTestActivity(t, d, ctx, teamID, ownerID, "Вторая работа")
	if err := d.AssignActivityProject(ctx, appmodel.AssignActivityProjectRequest{TeamID: teamID, ActivityID: firstActivity, ProjectID: first.ID, CallerID: ownerID}); err != nil {
		t.Fatal(err)
	}
	if err := d.AssignActivityProject(ctx, appmodel.AssignActivityProjectRequest{TeamID: teamID, ActivityID: secondActivity, ProjectID: second.ID, CallerID: ownerID}); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	seedClosedSession(t, d, teamID, ownerID, firstActivity, day.Add(9*time.Hour))
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO sessions (activity_id, team_id, user_id, start_at, end_at, accumulated_seconds)
		 VALUES (?, ?, ?, ?, ?, 7200)`,
		secondActivity, teamID, ownerID, FormatTime(day.Add(11*time.Hour)), FormatTime(day.Add(13*time.Hour))); err != nil {
		t.Fatal(err)
	}

	fact, err := d.TrackedSecondsByProject(ctx, appmodel.PersonDayQuery{TeamID: teamID, Days: []time.Time{day, day.AddDate(0, 0, 1)}, Now: day.AddDate(0, 0, 2)})
	if err != nil {
		t.Fatal(err)
	}
	if fact[first.ID] != 3600 || fact[second.ID] != 7200 {
		t.Fatalf("the fact per project is %v, want an hour on the first and two hours on the second", fact)
	}
}
