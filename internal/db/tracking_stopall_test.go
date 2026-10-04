package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestStopActiveSessionsReturnsCommittedTransitionSnapshots(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Stop all", "stop-all-snapshots")
	ownerID := teamOwner(t, d, teamID)
	ctx = requestctx.WithActor(ctx, ownerID)
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "deep work"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	session, err := d.StartSession(ctx, appmodel.TimerStartRequest{TeamID: teamID, ActivityID: activity.ID, At: start, Note: ""})
	if err != nil {
		t.Fatal(err)
	}
	end := start.Add(35 * time.Minute)

	stopped, err := d.StopActiveSessions(ctx, appmodel.TimerStopAllRequest{TeamID: teamID, At: end})
	if err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 1 {
		t.Fatalf("stopped sessions = %d, want 1", len(stopped))
	}
	got := stopped[0]
	if got.ID != session.ID || got.ActivityID != activity.ID || got.UserID != ownerID ||
		!got.StartAt.Equal(start) || got.EndAt == nil || !got.EndAt.Equal(end) || got.AccumulatedSeconds != 35*60 {
		t.Fatalf("stopped session snapshot = %+v", got)
	}
	stored, err := d.GetSession(ctx, appmodel.SessionLookupQuery{TeamID: teamID, SessionID: session.ID})
	if err != nil {
		t.Fatal(err)
	}
	if stored.EndAt == nil || !stored.EndAt.Equal(end) || stored.AccumulatedSeconds != got.AccumulatedSeconds {
		t.Fatalf("stored session = %+v, want committed snapshot %+v", stored, got)
	}
}
