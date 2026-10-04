package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestProjectUsageQueriesIgnoreCrossWorkspaceRelationships(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamA := seedTeam(t, d, "Project owner", "project-owner")
	ownerA := teamOwner(t, d, teamA)
	teamB := seedTeam(t, d, "Session owner", "session-owner")
	ownerB := teamOwner(t, d, teamB)
	project, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamA, CallerID: ownerA, Name: "Private project", Slug: "private-project", Color: ""})
	if err != nil {
		t.Fatal(err)
	}
	rate := 1000
	if err := d.SetProjectRate(ctx, appmodel.ProjectRateRequest{TeamID: teamA, ProjectID: project.ID, CallerID: ownerA, RateCents: &rate}); err != nil {
		t.Fatal(err)
	}
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamB, CallerID: ownerB, Name: "Foreign activity"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`UPDATE activities SET project_id = ? WHERE id = ? AND team_id = ?`, project.ID, activity.ID, teamB); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	if _, err := d.CreateClosedSession(requestctx.WithActor(ctx, ownerB), appmodel.TimerAddRequest{TeamID: teamB, ActivityID: activity.ID, Start: start, End: start.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	sessions, err := d.ProjectSessions(ctx, teamB, project.ID, start.Add(-time.Hour), start.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("ProjectSessions returned %d cross-workspace sessions", len(sessions))
	}
	total, err := d.ProjectTrackedTotal(ctx, teamB, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Fatalf("ProjectTrackedTotal = %d, want 0 for a foreign project", total)
	}
	spans, err := d.ProjectSpans(ctx, teamB, start.Add(-time.Hour), start.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 0 {
		t.Fatalf("ProjectSpans returned %d cross-workspace project spans", len(spans))
	}
	counts, err := d.ProjectActivityCounts(ctx, teamB)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := counts[project.ID]; ok {
		t.Fatalf("ProjectActivityCounts included foreign project %d", project.ID)
	}
	unbilled, err := d.Unbilled(ctx, teamB, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(unbilled) != 0 {
		t.Fatalf("Unbilled returned %d rows for a foreign project relationship", len(unbilled))
	}
	unassignedActivity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamA, CallerID: ownerA, Name: "local unassigned"})
	if err != nil {
		t.Fatal(err)
	}
	crossLinked, err := d.CreateClosedSession(requestctx.WithActor(ctx, ownerB), appmodel.TimerAddRequest{TeamID: teamB, ActivityID: activity.ID, Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`UPDATE sessions SET activity_id = ? WHERE id = ? AND team_id = ?`, unassignedActivity.ID, crossLinked.ID, teamB); err != nil {
		t.Fatal(err)
	}
	unassigned, err := d.UnassignedActivities(ctx, teamA)
	if err != nil {
		t.Fatal(err)
	}
	if len(unassigned) != 0 {
		t.Fatalf("UnassignedActivities returned %d rows from a foreign session relationship", len(unassigned))
	}
}
