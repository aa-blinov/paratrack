package db

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

func TestBuildInvoiceLinesRejectsMissingWorkspaceScope(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	var d *DB
	_, err := d.BuildInvoiceLinesFor(t.Context(), 0, start, start.AddDate(0, 0, 1), 0, 0)
	if !errors.Is(err, ErrInvalidInvoiceQuery) {
		t.Fatalf("BuildInvoiceLinesFor error = %v, want invalid query", err)
	}
}

func TestBuildInvoiceLinesRejectsMalformedSessionTime(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Malformed times", "malformed-times")
	ownerID := teamOwner(t, d, teamID)
	project, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Client", Slug: "client", Color: ""})
	if err != nil {
		t.Fatal(err)
	}
	rate := 1000
	if err := d.SetProjectRate(ctx, appmodel.ProjectRateRequest{TeamID: teamID, ProjectID: project.ID, CallerID: ownerID, RateCents: &rate}); err != nil {
		t.Fatal(err)
	}
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AssignActivityProject(ctx, appmodel.AssignActivityProjectRequest{TeamID: teamID, ActivityID: activity.ID, ProjectID: project.ID, CallerID: ownerID}); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	if _, err := d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activity.ID, Start: start, End: start.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`UPDATE sessions SET start_at = ? WHERE team_id = ?`, "2026-09-01T99:00:00Z", teamID); err != nil {
		t.Fatal(err)
	}

	_, err = d.BuildInvoiceLines(ctx, teamID, start.Add(-time.Hour), start.Add(24*time.Hour), 0)
	if err == nil || !strings.Contains(err.Error(), "parse invoice session") {
		t.Fatalf("BuildInvoiceLines error = %v, want malformed-session error", err)
	}
}
