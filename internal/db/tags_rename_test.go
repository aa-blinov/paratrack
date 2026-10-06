package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// seedRenameTagFixture gives one workspace an owner, a tagged session and the
// two tags the rename cases fight over.
func seedRenameTagFixture(t *testing.T, d *DB) (teamID, ownerID, sessionID, tagID, otherID int64) {
	t.Helper()
	ctx := t.Context()
	teamID = seedTeam(t, d, "Tag rename", "tag-rename")
	ownerID = teamOwner(t, d, teamID)
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "writing"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	session, err := d.CreateClosedSession(requestctx.WithActor(ctx, ownerID), appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activity.ID, Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	sessionID = session.ID
	tag, err := d.CreateTagForMember(ctx, appmodel.TagCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "deep-wrok"})
	if err != nil {
		t.Fatal(err)
	}
	tagID = tag.ID
	if err := d.AttachTagForMember(ctx, appmodel.SessionTagRequest{TeamID: teamID, CallerID: ownerID, SessionID: sessionID, Name: tag.Name}); err != nil {
		t.Fatal(err)
	}
	other, err := d.CreateTagForMember(ctx, appmodel.TagCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "meetings"})
	if err != nil {
		t.Fatal(err)
	}
	return teamID, ownerID, sessionID, tagID, other.ID
}

func TestRenameTagForManagerKeepsTaggedSessions(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID, ownerID, sessionID, tagID, _ := seedRenameTagFixture(t, d)

	renamed, err := d.RenameTagForManager(ctx, appmodel.TagRenameRequest{TeamID: teamID, CallerID: ownerID, TagID: tagID, Name: "deep-work"})
	if err != nil {
		t.Fatalf("rename tag: %v", err)
	}
	if renamed.ID != tagID || renamed.Name != "deep-work" {
		t.Fatalf("renamed tag = %+v, want id %d named deep-work", renamed, tagID)
	}
	bySession, err := d.TagsForSessions(ctx, appmodel.SessionTagsQuery{TeamID: teamID, SessionIDs: []int64{sessionID}})
	if err != nil {
		t.Fatal(err)
	}
	tags := bySession[sessionID]
	if len(tags) != 1 || tags[0].ID != tagID || tags[0].Name != "deep-work" {
		t.Fatalf("session tags after rename = %+v, want the renamed tag", tags)
	}
	counts, err := d.ListAllTagsWithCounts(ctx, appmodel.TagListQuery{TeamID: teamID})
	if err != nil {
		t.Fatal(err)
	}
	for _, counted := range counts {
		if counted.ID == tagID && counted.SessionCount != 1 {
			t.Fatalf("renamed tag session count = %d, want 1", counted.SessionCount)
		}
	}
}

func TestRenameTagForManagerNormalizesName(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID, ownerID, _, tagID, _ := seedRenameTagFixture(t, d)

	renamed, err := d.RenameTagForManager(ctx, appmodel.TagRenameRequest{TeamID: teamID, CallerID: ownerID, TagID: tagID, Name: "  Deep-Work  "})
	if err != nil {
		t.Fatalf("rename tag: %v", err)
	}
	if renamed.Name != "deep-work" {
		t.Fatalf("renamed name = %q, want normalized %q", renamed.Name, "deep-work")
	}
	// Saving the name it already carries is a no-op, not a collision.
	again, err := d.RenameTagForManager(ctx, appmodel.TagRenameRequest{TeamID: teamID, CallerID: ownerID, TagID: tagID, Name: "deep-work"})
	if err != nil || again.Name != "deep-work" {
		t.Fatalf("rename to current name = %+v, %v", again, err)
	}
}

func TestRenameTagForManagerRejectsNameOwnedByAnotherTag(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID, ownerID, sessionID, tagID, otherID := seedRenameTagFixture(t, d)

	if _, err := d.RenameTagForManager(ctx, appmodel.TagRenameRequest{TeamID: teamID, CallerID: ownerID, TagID: tagID, Name: "meetings"}); !errors.Is(err, appmodel.ErrTagNameTaken) {
		t.Fatalf("rename onto another tag = %v, want name taken", err)
	}
	tags, err := d.ListTags(ctx, appmodel.TagListQuery{TeamID: teamID})
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 {
		t.Fatalf("rejected rename changed the catalog: %+v", tags)
	}
	names := map[int64]string{}
	for _, tag := range tags {
		names[tag.ID] = tag.Name
	}
	if names[tagID] != "deep-wrok" || names[otherID] != "meetings" {
		t.Fatalf("rejected rename rewrote names: %v", names)
	}
	bySession, err := d.TagsForSessions(ctx, appmodel.SessionTagsQuery{TeamID: teamID, SessionIDs: []int64{sessionID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(bySession[sessionID]) != 1 || bySession[sessionID][0].Name != "deep-wrok" {
		t.Fatalf("rejected rename moved the session tag: %+v", bySession[sessionID])
	}
}

func TestRenameTagForManagerRejectsUnusableInput(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID, ownerID, _, tagID, _ := seedRenameTagFixture(t, d)

	if _, err := d.RenameTagForManager(ctx, appmodel.TagRenameRequest{TeamID: teamID, CallerID: ownerID, TagID: tagID, Name: "   "}); err == nil {
		t.Fatal("rename to a blank name should fail, got nil")
	}
	if _, err := d.RenameTagForManager(ctx, appmodel.TagRenameRequest{TeamID: teamID, CallerID: ownerID, TagID: tagID + 1000, Name: "deep-work"}); !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("rename of a missing tag = %v, want tag not found", err)
	}
}

func TestRenameTagForManagerRequiresManagerRoleInAnotherWorkspace(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID, ownerID, _, tagID, _ := seedRenameTagFixture(t, d)
	memberID := seedProjectTestUser(t, d, "tag-rename-member")
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, memberID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	outsiderID := teamOwner(t, d, seedTeam(t, d, "Other rename", "other-rename"))

	if _, err := d.RenameTagForManager(ctx, appmodel.TagRenameRequest{TeamID: teamID, CallerID: memberID, TagID: tagID, Name: "deep-work"}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("member rename = %v, want forbidden", err)
	}
	if _, err := d.RenameTagForManager(ctx, appmodel.TagRenameRequest{TeamID: teamID, CallerID: outsiderID, TagID: tagID, Name: "deep-work"}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("outsider rename = %v, want forbidden", err)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `DELETE FROM memberships WHERE team_id = ? AND user_id = ?`, teamID, memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.RenameTagForManager(ctx, appmodel.TagRenameRequest{TeamID: teamID, CallerID: memberID, TagID: tagID, Name: "deep-work"}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("removed member rename = %v, want forbidden", err)
	}
	tag, err := d.RenameTagForManager(ctx, appmodel.TagRenameRequest{TeamID: teamID, CallerID: ownerID, TagID: tagID, Name: "deep-work"})
	if err != nil || tag.Name != "deep-work" {
		t.Fatalf("owner rename after rejections = %+v, %v", tag, err)
	}
}

func TestRenameTagForManagerCarriesSavedReportsAlong(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID, ownerID, _, tagID, _ := seedRenameTagFixture(t, d)

	if _, err := d.CreateSavedReport(ctx, appmodel.SavedReportCreateRequest{
		TeamID: teamID, ActorID: ownerID, Name: "week deep", Period: "week", Tag: "deep-wrok",
	}); err != nil {
		t.Fatalf("save preset on the tag: %v", err)
	}
	if _, err := d.RenameTagForManager(ctx, appmodel.TagRenameRequest{
		TeamID: teamID, CallerID: ownerID, TagID: tagID, Name: "deep-work",
	}); err != nil {
		t.Fatalf("rename tag: %v", err)
	}
	var presetTag string
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT tag FROM saved_reports WHERE team_id = ? AND name = ?`, teamID, "week deep").Scan(&presetTag); err != nil {
		t.Fatalf("read preset: %v", err)
	}
	if presetTag != "deep-work" {
		t.Fatalf("saved preset filter = %q, want deep-work; a rename must not silently empty it", presetTag)
	}
}
