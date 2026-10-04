package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestLegacyTagWritesRejectWorkspaceScope(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Tag write boundary", "tag-write-boundary")
	if _, err := d.createTag(ctx, legacyTagCreateRequest{TeamID: teamID, Name: "blocked"}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("legacy workspace tag create = %v, want forbidden", err)
	}
	if err := d.deleteTag(ctx, legacyTagDeleteRequest{TeamID: teamID, TagID: 1}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("legacy workspace tag delete = %v, want forbidden", err)
	}
	if err := d.attachTag(ctx, legacySessionTagRequest{TeamID: teamID, SessionID: 1, Name: "blocked"}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("legacy workspace tag attach = %v, want forbidden", err)
	}
	if err := d.detachTag(ctx, legacySessionTagRequest{TeamID: teamID, SessionID: 1, Name: "blocked"}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("legacy workspace tag detach = %v, want forbidden", err)
	}
	if err := d.setTagsForSession(ctx, legacySessionTagsRequest{TeamID: teamID, SessionID: 1, Names: []string{"blocked"}}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("legacy workspace tag replacement = %v, want forbidden", err)
	}
	ownerID := teamOwner(t, d, teamID)
	workspaceTag, err := d.CreateTagForMember(ctx, appmodel.TagCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "shared-name"})
	if err != nil {
		t.Fatal(err)
	}
	legacyTag, err := d.createTag(ctx, legacyTagCreateRequest{TeamID: 0, Name: "shared-name"})
	if err != nil {
		t.Fatal(err)
	}
	if legacyTag.ID == workspaceTag.ID || legacyTag.TeamID != 0 {
		t.Fatalf("legacy tag resolved a workspace tag: workspace=%+v legacy=%+v", workspaceTag, legacyTag)
	}
}

func TestSessionTags_RequireCurrentWorkspaceMembership(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")
	ownerID := teamOwner(t, d, teamID)
	memberID := seedProjectTestUser(t, d, "tag-member")
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, memberID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "work"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	session, err := d.CreateClosedSession(requestctx.WithActor(ctx, ownerID), appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activity.ID, Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AttachTagForMember(ctx, appmodel.SessionTagRequest{TeamID: teamID, CallerID: memberID, SessionID: session.ID, Name: "shared"}); err != nil {
		t.Fatalf("current member attach: %v", err)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `DELETE FROM memberships WHERE team_id = ? AND user_id = ?`, teamID, memberID); err != nil {
		t.Fatal(err)
	}
	if err := d.DetachTagForMember(ctx, appmodel.SessionTagRequest{TeamID: teamID, CallerID: memberID, SessionID: session.ID, Name: "shared"}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("removed member detach error = %v, want forbidden", err)
	}
	if _, err := d.CreateTagForMember(ctx, appmodel.TagCreateRequest{TeamID: teamID, CallerID: memberID, Name: "orphan"}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("removed member create error = %v, want forbidden", err)
	}
	var links int
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM session_tags WHERE session_id = ?`, session.ID).Scan(&links); err != nil {
		t.Fatal(err)
	}
	if links != 1 {
		t.Fatalf("rejected detach changed session tags: links=%d", links)
	}
	tags, err := d.ListTags(ctx, teamID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 || tags[0].Name != "shared" {
		t.Fatalf("rejected tag creation changed catalog: %+v", tags)
	}
}
