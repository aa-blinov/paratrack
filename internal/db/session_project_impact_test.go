package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// Moving one entry to a project moves only that entry while the activity already has a
// project. An activity with no project takes the project from the change, and every entry
// of it without a project of its own follows, which the confirmation has to name.
func TestSessionProjectImpactCountsWhatTheChangeMoves(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "impact", "impact")
	ownerID := teamOwner(t, d, teamID)
	ownerCtx := requestctx.WithActor(ctx, ownerID)
	newProject := func(name string) int64 {
		t.Helper()
		project, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: name, Color: "#7c3aed"})
		if err != nil {
			t.Fatal(err)
		}
		return project.ID
	}
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "impact"})
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	add := func(hour, minutes int, projectID int64) int64 {
		t.Helper()
		start := day.Add(time.Duration(hour) * time.Hour)
		session, err := d.CreateClosedSession(ownerCtx, appmodel.TimerAddRequest{
			TeamID: teamID, ActivityID: activity.ID, Start: start, End: start.Add(time.Duration(minutes) * time.Minute), Note: "impact", ProjectID: projectID,
		})
		if err != nil {
			t.Fatal(err)
		}
		return session.ID
	}
	first := add(9, 60, 0)
	add(11, 30, 0)

	// No project on the activity yet: the change binds it, so both entries move, 90 minutes
	// between them. A project tagged on a session of an unbound activity binds it the same way,
	// which is why the test does not tag one first.
	target := newProject("target")
	impact, err := d.SessionProjectImpact(ctx, appmodel.SessionProjectImpactQuery{TeamID: teamID, SessionID: first, ProjectID: target})
	if err != nil {
		t.Fatal(err)
	}
	if !impact.Binds || impact.Sessions != 2 || impact.Seconds != 5400 {
		t.Fatalf("binding change = %+v, want binds, 2 entries, 5400 seconds", impact)
	}

	// Once the activity has a project, the change moves only the entry it was made on.
	if err := d.AssignActivityProject(ownerCtx, appmodel.AssignActivityProjectRequest{TeamID: teamID, ActivityID: activity.ID, ProjectID: target, CallerID: ownerID}); err != nil {
		t.Fatalf("assign activity project: %v", err)
	}
	other := newProject("other")
	impact, err = d.SessionProjectImpact(ctx, appmodel.SessionProjectImpactQuery{TeamID: teamID, SessionID: first, ProjectID: other})
	if err != nil {
		t.Fatal(err)
	}
	if impact.Binds || impact.Sessions != 1 {
		t.Fatalf("change on an activity with a project = %+v, want the entry alone", impact)
	}
}
