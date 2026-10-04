package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestFocusActivityReturnsIDForNewSession(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Focus", "focus-session")
	ownerID := teamOwner(t, d, teamID)
	ctx = requestctx.WithActor(ctx, ownerID)
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "deep work"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)

	first, err := d.FocusActivity(ctx, appmodel.TimerFocusRequest{TeamID: teamID, ActivityID: activity.ID, At: now})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Started || first.StartedSessionID <= 0 {
		t.Fatalf("new focus result = %+v, want a started session ID", first)
	}
	session, err := d.GetSession(ctx, appmodel.SessionLookupQuery{TeamID: teamID, SessionID: first.StartedSessionID})
	if err != nil {
		t.Fatal(err)
	}
	if session.ActivityID != activity.ID || !session.StartAt.Equal(now) {
		t.Fatalf("focused session = %+v, want activity %d at %s", session, activity.ID, now)
	}

	second, err := d.FocusActivity(ctx, appmodel.TimerFocusRequest{TeamID: teamID, ActivityID: activity.ID, At: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if second.Started || second.StartedSessionID != 0 {
		t.Fatalf("focusing the already active activity created another session: %+v", second)
	}
}
