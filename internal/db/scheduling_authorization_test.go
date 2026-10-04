package db

import (
	"errors"
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

func TestUpsertScheduleEntryRequiresCurrentManager(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Schedule authorization", "schedule-authorization")
	ownerID := teamOwner(t, d, teamID)
	outsiderID := seedProjectTestUser(t, d, "schedule-outsider")
	project, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Schedule project", Slug: "schedule-project", Color: "#123456"})
	if err != nil {
		t.Fatal(err)
	}
	day := time.Now().UTC()
	if err := d.UpsertScheduleEntry(ctx, appmodel.ScheduleCellRequest{
		TeamID: teamID, ActorID: outsiderID, UserID: ownerID, ProjectID: project.ID, Day: day, Minutes: 60,
	}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("outsider scheduling for a member = %v, want forbidden", err)
	}
	if err := d.UpsertScheduleEntry(ctx, appmodel.ScheduleCellRequest{
		TeamID: teamID, ActorID: ownerID, UserID: ownerID, ProjectID: project.ID, Day: day, Minutes: 60,
	}); err != nil {
		t.Fatalf("owner scheduling for a member: %v", err)
	}
}
