package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
	"github.com/aa-blinov/paratrack/internal/webhookport"
)

func TestSessionEditAndDelete_RequireCurrentWorkspaceMembership(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")
	ownerID := teamOwner(t, d, teamID)
	memberID := seedProjectTestUser(t, d, "session-member")
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, memberID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "work"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	memberCtx := requestctx.WithScope(requestctx.WithActor(ctx, memberID), memberID)
	session, err := d.CreateClosedSession(memberCtx, appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activity.ID, Start: now.Add(-time.Hour), End: now, Note: "original"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.RemoveTeamMember(ctx, appmodel.TeamMemberRemovalRequest{TeamID: teamID, TargetUserID: memberID, CallerID: ownerID, LeftAt: now}); err != nil {
		t.Fatal(err)
	}

	update := appmodel.SessionUpdateRequest{TeamID: teamID, CallerID: memberID, SessionID: session.ID, Update: appmodel.SessionUpdate{Note: "unauthorized", UpdatedAt: now}}
	if err := d.UpdateSessionFields(memberCtx, update); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("removed member update error = %v, want forbidden", err)
	}
	if err := d.DeleteSession(memberCtx, appmodel.SessionDeleteRequest{TeamID: teamID, CallerID: memberID, SessionID: session.ID}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("removed member delete error = %v, want forbidden", err)
	}
	var note string
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT note FROM sessions WHERE id = ?`, session.ID).Scan(&note); err != nil {
		t.Fatal(err)
	}
	if note != "original" {
		t.Fatalf("rejected edit changed session note to %q", note)
	}
}

func TestDemotedManagerCannotEditOrDeleteAnotherMembersSession(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Session manager scope", "session-manager-scope")
	ownerID := teamOwner(t, d, teamID)
	managerID := seedProjectTestUser(t, d, "session-manager")
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'admin', ?)`,
		teamID, managerID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "manager scope work"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	ownerCtx := requestctx.WithActor(ctx, ownerID)
	session, err := d.CreateClosedSession(ownerCtx, appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activity.ID, Start: now.Add(-time.Hour), End: now, Note: "owner session"})
	if err != nil {
		t.Fatal(err)
	}
	update := appmodel.SessionUpdateRequest{TeamID: teamID, CallerID: managerID, SessionID: session.ID, Update: appmodel.SessionUpdate{Note: "edited by manager", UpdatedAt: now}}
	if err := d.UpdateSessionFields(ctx, update); err != nil {
		t.Fatalf("current manager editing another member's session: %v", err)
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`UPDATE memberships SET role = 'member' WHERE team_id = ? AND user_id = ?`, teamID, managerID); err != nil {
		t.Fatal(err)
	}
	update.Update.Note = "stale manager edit"
	if err := d.UpdateSessionFields(ctx, update); !errors.Is(err, ErrNotFound) {
		t.Fatalf("demoted manager editing another member's session = %v, want not found", err)
	}
	if err := d.DeleteSession(ctx, appmodel.SessionDeleteRequest{TeamID: teamID, CallerID: managerID, SessionID: session.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("demoted manager deleting another member's session = %v, want not found", err)
	}
}

func TestCreateSession_RequiresCurrentWorkspaceMembership(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")
	ownerID := teamOwner(t, d, teamID)
	outsiderID := seedProjectTestUser(t, d, "session-outsider")
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "work"})
	if err != nil {
		t.Fatal(err)
	}

	outsiderCtx := requestctx.WithActor(ctx, outsiderID)
	if _, err := d.StartSession(outsiderCtx, appmodel.TimerStartRequest{TeamID: teamID, ActivityID: activity.ID, At: time.Now().UTC(), Note: "unauthorized"}); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("non-member session creation error = %v, want not found", err)
	}
	var count int
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE team_id = ?`, teamID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rejected session creation inserted %d sessions", count)
	}
}

func TestLegacySessionCreationCannotTargetWorkspaceActivity(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Legacy create", "legacy-session-create")
	ownerID := teamOwner(t, d, teamID)
	workspaceActivity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "workspace work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.createLegacySession(ctx, legacySessionCreateRequest{ActivityID: workspaceActivity.ID, At: time.Now().UTC(), Note: "cross-scope"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("legacy session for workspace activity = %v, want not found", err)
	}
	var count int
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE team_id = ?`, teamID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("legacy session creation inserted %d workspace sessions", count)
	}

	legacyActivity, err := d.getOrCreateActivity(ctx, legacyActivityRequest{TeamID: 0, Name: "legacy work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.createLegacySession(ctx, legacySessionCreateRequest{ActivityID: legacyActivity.ID, At: time.Now().UTC(), Note: "legacy"}); err != nil {
		t.Fatalf("legacy session for unscoped activity: %v", err)
	}
}

func TestSessionTransitionsRejectUnscopedWorkspace(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Unscoped session", "unscoped-session-transition")
	ownerID := teamOwner(t, d, teamID)
	ctx = requestctx.WithActor(ctx, ownerID)
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "protected work"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	session, err := d.StartSession(ctx, appmodel.TimerStartRequest{TeamID: teamID, ActivityID: activity.ID, At: now, Note: ""})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.StopActiveSessions(ctx, appmodel.TimerStopAllRequest{TeamID: 0, At: now.Add(time.Minute)}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("unscoped stop-all error = %v, want forbidden", err)
	}
	if _, err := d.FocusActivity(ctx, appmodel.TimerFocusRequest{TeamID: 0, ActivityID: activity.ID, At: now.Add(time.Minute)}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("unscoped focus error = %v, want forbidden", err)
	}
	stored, err := d.GetSession(ctx, teamID, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.EndAt != nil {
		t.Fatalf("rejected unscoped transition stopped session at %s", stored.EndAt)
	}
}

func TestRemovingMemberEmitsTransactionalSessionStoppedEvent(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Removal webhook", "removal-webhook")
	ownerID := teamOwner(t, d, teamID)
	memberID := seedProjectTestUser(t, d, "removal-webhook-member")
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, memberID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateWebhook(ctx, appmodel.WebhookRegistrationCommand{TeamID: teamID, CallerID: ownerID, URL: "https://example.test/hook", Secret: "secret", Events: "session.stopped"}); err != nil {
		t.Fatal(err)
	}
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "work"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	memberCtx := requestctx.WithScope(requestctx.WithActor(ctx, memberID), memberID)
	session, err := d.StartSession(memberCtx, appmodel.TimerStartRequest{TeamID: teamID, ActivityID: activity.ID, At: start})
	if err != nil {
		t.Fatal(err)
	}
	stoppedAt := start.Add(time.Hour)
	if err := d.RemoveTeamMember(ctx, appmodel.TeamMemberRemovalRequest{TeamID: teamID, TargetUserID: memberID, CallerID: ownerID, LeftAt: stoppedAt}); err != nil {
		t.Fatal(err)
	}
	stoppedSession, err := d.GetSession(ctx, teamID, session.ID)
	if err != nil || stoppedSession.EndAt == nil || !stoppedSession.EndAt.Equal(stoppedAt) {
		t.Fatalf("session after member removal = %+v, err=%v, want ended at %s", stoppedSession, err, stoppedAt)
	}

	event, ok, err := d.ClaimWebhookEvent(ctx)
	if err != nil || !ok {
		t.Fatalf("ClaimWebhookEvent = (%+v, %v, %v), want a committed stop event", event, ok, err)
	}
	decoded, err := webhookport.DecodeEvent(webhookport.EventName(event.Event), event.Payload)
	stopped, typed := decoded.(*webhookport.SessionStoppedEvent)
	if err != nil || !typed || stopped.SessionID != session.ID || stopped.ActivityID != activity.ID || len(event.WebhookIDs) != 1 {
		t.Fatalf("outbox event = %+v, decoded=%+v, err=%v", event, decoded, err)
	}
}

func TestEditingOpenSessionClosedEmitsTransactionalStoppedEvent(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Edited stop webhook", "edited-stop-webhook")
	ownerID := teamOwner(t, d, teamID)
	ctx = requestctx.WithActor(ctx, ownerID)
	if _, err := d.CreateWebhook(ctx, appmodel.WebhookRegistrationCommand{TeamID: teamID, CallerID: ownerID, URL: "https://example.test/hook", Secret: "secret", Events: "session.stopped"}); err != nil {
		t.Fatal(err)
	}
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "work"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC)
	session, err := d.StartSession(ctx, appmodel.TimerStartRequest{TeamID: teamID, ActivityID: activity.ID, At: start})
	if err != nil {
		t.Fatal(err)
	}
	end := start.Add(time.Hour)
	seconds := 3600
	if err := d.UpdateSessionFields(ctx, appmodel.SessionUpdateRequest{
		TeamID: teamID, CallerID: ownerID, SessionID: session.ID,
		Update: SessionUpdate{EndAt: &end, AccumulatedSeconds: &seconds, UpdatedAt: end},
	}); err != nil {
		t.Fatal(err)
	}

	event, ok, err := d.ClaimWebhookEvent(ctx)
	if err != nil || !ok {
		t.Fatalf("ClaimWebhookEvent = (%+v, %v, %v), want a committed stop event", event, ok, err)
	}
	decoded, err := webhookport.DecodeEvent(webhookport.EventName(event.Event), event.Payload)
	stopped, typed := decoded.(*webhookport.SessionStoppedEvent)
	if err != nil || !typed || stopped.SessionID != session.ID || stopped.ActivityID != activity.ID || len(event.WebhookIDs) != 1 {
		t.Fatalf("outbox event = %+v, decoded=%+v, err=%v", event, decoded, err)
	}
}
