package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestUpdateSessionDurationUsesLockedStartAndPersistsResolvedInterval(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Session edit intent", "session-edit-intent")
	ownerID := teamOwner(t, d, teamID)
	ctx = requestctx.WithActor(ctx, ownerID)
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "work"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC)
	session, err := d.CreateClosedSession(ctx, appmodel.TimerAddRequest{
		TeamID: teamID, ActivityID: activity.ID, Start: start, End: start.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	lockedStart := start.Add(2 * time.Hour)
	seconds := 45 * 60
	updatedAt := start.Add(4 * time.Hour)
	if err := d.UpdateSessionFields(ctx, appmodel.SessionUpdateRequest{
		TeamID: teamID, CallerID: ownerID, SessionID: session.ID, DurationSeconds: &seconds,
		Update: appmodel.SessionUpdate{StartAt: &lockedStart, UpdatedAt: updatedAt},
	}); err != nil {
		t.Fatal(err)
	}
	var gotStart, gotEnd string
	var gotSeconds int
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT start_at, end_at, accumulated_seconds FROM sessions WHERE id = ?`, session.ID,
	).Scan(&gotStart, &gotEnd, &gotSeconds); err != nil {
		t.Fatal(err)
	}
	parsedStart, err := ScanTime(gotStart)
	if err != nil {
		t.Fatal(err)
	}
	parsedEnd, err := ScanTime(gotEnd)
	if err != nil {
		t.Fatal(err)
	}
	if !parsedStart.Equal(lockedStart) || !parsedEnd.Equal(lockedStart.Add(time.Duration(seconds)*time.Second)) || gotSeconds != seconds {
		t.Fatalf("persisted session interval = start %s end %s seconds %d", gotStart, gotEnd, gotSeconds)
	}
}

func TestUpdateSessionRejectsInvalidResolvedIntervalWithoutWriting(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Invalid session edit", "invalid-session-edit")
	ownerID := teamOwner(t, d, teamID)
	ctx = requestctx.WithActor(ctx, ownerID)
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "work"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	session, err := d.CreateClosedSession(ctx, appmodel.TimerAddRequest{
		TeamID: teamID, ActivityID: activity.ID, Start: start, End: end,
	})
	if err != nil {
		t.Fatal(err)
	}
	invalidStart := end.Add(time.Hour)
	err = d.UpdateSessionFields(ctx, appmodel.SessionUpdateRequest{
		TeamID: teamID, CallerID: ownerID, SessionID: session.ID, RecomputeDuration: true,
		Update: appmodel.SessionUpdate{StartAt: &invalidStart, UpdatedAt: end.Add(time.Hour)},
	})
	if !errors.Is(err, appmodel.ErrInvalidSessionPeriod) {
		t.Fatalf("invalid resolved interval error = %v, want %v", err, appmodel.ErrInvalidSessionPeriod)
	}
	current, err := d.GetSession(ctx, appmodel.SessionLookupQuery{TeamID: teamID, SessionID: session.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !current.StartAt.Equal(start) || current.EndAt == nil || !current.EndAt.Equal(end) || current.AccumulatedSeconds != 3600 {
		t.Fatalf("rejected edit changed session: %+v", current)
	}
}
