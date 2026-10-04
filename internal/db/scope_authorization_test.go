package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestTeamSessionWriteRequiresActor(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Actor required", "actor-required")
	ownerID := teamOwner(t, d, teamID)
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.StartSession(ctx, appmodel.TimerStartRequest{TeamID: teamID, ActivityID: activity.ID, At: time.Now().UTC()}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("team session write without actor = %v, want forbidden", err)
	}
	if _, err := d.StartSession(requestctx.WithActor(ctx, ownerID), appmodel.TimerStartRequest{TeamID: teamID, ActivityID: activity.ID, At: time.Now().UTC()}); err != nil {
		t.Fatalf("team session write with current owner = %v", err)
	}
}
