package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestSessionTransitionUpdatedAtUsesSuppliedTransitionTime(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Clock", "session-transition-clock")
	ownerID := teamOwner(t, d, teamID)
	ctx = requestctx.WithActor(ctx, ownerID)
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "work"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	session, err := d.StartSession(ctx, appmodel.TimerStartRequest{TeamID: teamID, ActivityID: activity.ID, At: start, Note: ""})
	if err != nil {
		t.Fatal(err)
	}

	pauseAt := start.Add(10 * time.Minute)
	paused, err := d.PauseSession(ctx, appmodel.TimerSessionRequest{TeamID: teamID, SessionID: session.ID, At: pauseAt})
	if err != nil {
		t.Fatal(err)
	}
	if !paused.UpdatedAt.Equal(pauseAt) {
		t.Fatalf("pause updated_at = %s, want %s", paused.UpdatedAt, pauseAt)
	}

	resumeAt := pauseAt.Add(5 * time.Minute)
	resumed, err := d.ResumeSession(ctx, appmodel.TimerSessionRequest{TeamID: teamID, SessionID: session.ID, At: resumeAt})
	if err != nil {
		t.Fatal(err)
	}
	if !resumed.UpdatedAt.Equal(resumeAt) {
		t.Fatalf("resume updated_at = %s, want %s", resumed.UpdatedAt, resumeAt)
	}

	stopAt := resumeAt.Add(20 * time.Minute)
	stopped, err := d.UpdateSessionEnd(ctx, appmodel.TimerStopRequest{TeamID: teamID, SessionID: session.ID, At: stopAt})
	if err != nil {
		t.Fatal(err)
	}
	if !stopped.UpdatedAt.Equal(stopAt) {
		t.Fatalf("stop updated_at = %s, want %s", stopped.UpdatedAt, stopAt)
	}

	reopenAt := stopAt.Add(time.Minute)
	_, err = d.ReopenSession(ctx, appmodel.TimerReopenRequest{
		TeamID: teamID, SessionID: session.ID, At: reopenAt, ExpectedEndAt: stopped.EndAt.Add(time.Second),
	})
	if !errors.Is(err, model.ErrSessionReopenExpired) {
		t.Fatalf("reopen with stale end time error = %v, want %v", err, model.ErrSessionReopenExpired)
	}
	reopened, err := d.ReopenSession(ctx, appmodel.TimerReopenRequest{
		TeamID: teamID, SessionID: session.ID, At: reopenAt, ExpectedEndAt: *stopped.EndAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.UpdatedAt.Equal(reopenAt) {
		t.Fatalf("reopen updated_at = %s, want %s", reopened.UpdatedAt, reopenAt)
	}
}
