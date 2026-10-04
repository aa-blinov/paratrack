package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestCreateTag_Idempotent(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	a, err := d.createTag(ctx, legacyTagCreateRequest{TeamID: 0, Name: "deep-work"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := d.createTag(ctx, legacyTagCreateRequest{TeamID: 0, Name: "deep-work"})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID {
		t.Errorf("createTag on existing name: a.ID=%d, b.ID=%d (should be equal)", a.ID, b.ID)
	}
	// Mixed-case input must collapse to the same tag — the COLLATE
	// NOCASE column guarantees it but createTag also normalises on the
	// way in so the *output* name is the canonical lowercase.
	c, err := d.createTag(ctx, legacyTagCreateRequest{TeamID: 0, Name: "Deep-Work"})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != c.ID || c.Name != "deep-work" {
		t.Errorf("mixed-case 'Deep-Work' should resolve to %d (name=%q), got %d (name=%q)",
			a.ID, "deep-work", c.ID, c.Name)
	}
}

func TestCreateTag_TrimsWhitespace(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	t1, err := d.createTag(ctx, legacyTagCreateRequest{TeamID: 0, Name: "  spaced  "})
	if err != nil {
		t.Fatal(err)
	}
	if t1.Name != "spaced" {
		t.Errorf("created tag name = %q, want normalized name %q", t1.Name, "spaced")
	}
}

func TestCreateTag_RejectsEmpty(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	if _, err := d.createTag(ctx, legacyTagCreateRequest{TeamID: 0, Name: "   "}); err == nil {
		t.Error("createTag on whitespace-only should fail, got nil")
	}
}

func TestAttachTag_AutoCreates(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	act, _ := d.getOrCreateActivity(ctx, legacyActivityRequest{TeamID: 0, Name: "writing"})
	s, err := d.createLegacySession(ctx, legacySessionCreateRequest{ActivityID: act.ID, At: time.Now(), Note: ""})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.attachTag(ctx, legacySessionTagRequest{TeamID: 0, SessionID: s.ID, Name: "deep-work"}); err != nil {
		t.Fatalf("attachTag should auto-create: %v", err)
	}
	// Tag should exist now.
	tags, err := d.listTagsForSession(ctx, 0, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 || tags[0].Name != "deep-work" {
		t.Errorf("attached tag wrong: %+v", tags)
	}
}

func TestAttachTag_Idempotent(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	act, _ := d.getOrCreateActivity(ctx, legacyActivityRequest{TeamID: 0, Name: "reading"})
	s, _ := d.createLegacySession(ctx, legacySessionCreateRequest{ActivityID: act.ID, At: time.Now(), Note: ""})
	_ = d.attachTag(ctx, legacySessionTagRequest{TeamID: 0, SessionID: s.ID, Name: "morning"})
	if err := d.attachTag(ctx, legacySessionTagRequest{TeamID: 0, SessionID: s.ID, Name: "morning"}); err != nil {
		t.Errorf("double attach should be no-op, got: %v", err)
	}
	tags, _ := d.listTagsForSession(ctx, 0, s.ID)
	if len(tags) != 1 {
		t.Errorf("expected 1 tag after double attach, got %d", len(tags))
	}
}

func TestDetachTag_KeepsTagAlive(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	act, _ := d.getOrCreateActivity(ctx, legacyActivityRequest{TeamID: 0, Name: "work"})
	s, _ := d.createLegacySession(ctx, legacySessionCreateRequest{ActivityID: act.ID, At: time.Now(), Note: ""})
	_, _ = d.createTag(ctx, legacyTagCreateRequest{TeamID: 0, Name: "office"})
	if err := d.attachTag(ctx, legacySessionTagRequest{TeamID: 0, SessionID: s.ID, Name: "office"}); err != nil {
		t.Fatal(err)
	}
	if err := d.detachTag(ctx, legacySessionTagRequest{TeamID: 0, SessionID: s.ID, Name: "office"}); err != nil {
		t.Fatal(err)
	}
	tags, _ := d.listTagsForSession(ctx, 0, s.ID)
	if len(tags) != 0 {
		t.Errorf("session still has tags after detach: %v", tags)
	}
	// Tag itself should still exist.
	var count int
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM tags WHERE team_id IS NULL AND name = 'office'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Error("detach removed the tag from the catalogue, expected it to remain")
	}
}

func TestSetTagsForSession_ReplacesAll(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	act, _ := d.getOrCreateActivity(ctx, legacyActivityRequest{TeamID: 0, Name: "study"})
	s, _ := d.createLegacySession(ctx, legacySessionCreateRequest{ActivityID: act.ID, At: time.Now(), Note: ""})
	_ = d.attachTag(ctx, legacySessionTagRequest{TeamID: 0, SessionID: s.ID, Name: "old1"})
	_ = d.attachTag(ctx, legacySessionTagRequest{TeamID: 0, SessionID: s.ID, Name: "old2"})

	if err := d.setTagsForSession(ctx, legacySessionTagsRequest{TeamID: 0, SessionID: s.ID, Names: []string{"new1", "new2", "new3"}}); err != nil {
		t.Fatal(err)
	}
	tags, _ := d.listTagsForSession(ctx, 0, s.ID)
	got := map[string]bool{}
	for _, tg := range tags {
		got[tg.Name] = true
	}
	if len(got) != 3 || !got["new1"] || !got["new2"] || !got["new3"] {
		t.Errorf("after replace, tags = %v; want only new1/new2/new3", tags)
	}
	if got["old1"] || got["old2"] {
		t.Error("setTagsForSession should have removed old tags")
	}
}

func TestSetTagsForSession_EmptyClears(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	act, _ := d.getOrCreateActivity(ctx, legacyActivityRequest{TeamID: 0, Name: "calm"})
	s, _ := d.createLegacySession(ctx, legacySessionCreateRequest{ActivityID: act.ID, At: time.Now(), Note: ""})
	_ = d.attachTag(ctx, legacySessionTagRequest{TeamID: 0, SessionID: s.ID, Name: "temp"})
	if err := d.setTagsForSession(ctx, legacySessionTagsRequest{TeamID: 0, SessionID: s.ID, Names: nil}); err != nil {
		t.Fatal(err)
	}
	tags, _ := d.listTagsForSession(ctx, 0, s.ID)
	if len(tags) != 0 {
		t.Errorf("empty set should clear all tags, got %v", tags)
	}
}

func TestTagsForSessions_BatchedAcrossMany(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	act, _ := d.getOrCreateActivity(ctx, legacyActivityRequest{TeamID: 0, Name: "batch"})
	var sids []int64
	for i := 0; i < 5; i++ {
		s, _ := d.createLegacySession(ctx, legacySessionCreateRequest{ActivityID: act.ID, At: time.Now(), Note: ""})
		sids = append(sids, s.ID)
	}
	// Tag session 0 with 'a', 1 with 'b', 2 with both.
	_ = d.attachTag(ctx, legacySessionTagRequest{TeamID: 0, SessionID: sids[0], Name: "a"})
	_ = d.attachTag(ctx, legacySessionTagRequest{TeamID: 0, SessionID: sids[1], Name: "b"})
	_ = d.attachTag(ctx, legacySessionTagRequest{TeamID: 0, SessionID: sids[2], Name: "a"})
	_ = d.attachTag(ctx, legacySessionTagRequest{TeamID: 0, SessionID: sids[2], Name: "b"})

	got, err := d.TagsForSessions(ctx, appmodel.SessionTagsQuery{TeamID: 0, SessionIDs: sids})
	if err != nil {
		t.Fatal(err)
	}
	if len(got[sids[0]]) != 1 || got[sids[0]][0].Name != "a" {
		t.Errorf("session 0 tags = %v", got[sids[0]])
	}
	if len(got[sids[1]]) != 1 || got[sids[1]][0].Name != "b" {
		t.Errorf("session 1 tags = %v", got[sids[1]])
	}
	if len(got[sids[2]]) != 2 {
		t.Errorf("session 2 tags = %v (want 2)", got[sids[2]])
	}
	if len(got[sids[3]]) != 0 {
		t.Errorf("session 3 tags = %v (want empty)", got[sids[3]])
	}
	// Sessions not in the result should not panic.
	if _, ok := got[int64(99999)]; ok {
		t.Error("non-existent session leaked into result map")
	}
}

func TestDeleteTag_CascadesIntoSessionTags(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	act, _ := d.getOrCreateActivity(ctx, legacyActivityRequest{TeamID: 0, Name: "doomed"})
	s, _ := d.createLegacySession(ctx, legacySessionCreateRequest{ActivityID: act.ID, At: time.Now(), Note: ""})
	tag, _ := d.createTag(ctx, legacyTagCreateRequest{TeamID: 0, Name: "doomed"})
	if err := d.attachTag(ctx, legacySessionTagRequest{TeamID: 0, SessionID: s.ID, Name: "doomed"}); err != nil {
		t.Fatal(err)
	}
	if err := d.deleteTag(ctx, legacyTagDeleteRequest{TeamID: 0, TagID: tag.ID}); err != nil {
		t.Fatal(err)
	}
	tags, _ := d.listTagsForSession(ctx, 0, s.ID)
	if len(tags) != 0 {
		t.Errorf("session_tags should cascade: got %v", tags)
	}
}

func TestDeleteTag_LegacyPathCannotDeleteWorkspaceTag(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Scoped tag delete", "scoped-tag-delete")
	ownerID := teamOwner(t, d, teamID)
	tag, err := d.CreateTagForMember(ctx, appmodel.TagCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "keep"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.deleteTag(ctx, legacyTagDeleteRequest{TeamID: 0, TagID: tag.ID}); !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("legacy delete error = %v, want tag not found", err)
	}
	tags, err := d.ListTags(ctx, appmodel.TagListQuery{TeamID: teamID})
	if err != nil {
		t.Fatal(err)
	}
	for _, listed := range tags {
		if listed.ID == tag.ID {
			return
		}
	}
	t.Fatalf("workspace tag %d was deleted by legacy path", tag.ID)
}

func TestListAllTagsWithCounts_OrderedByPopularity(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Tag ranking", "tag-ranking")
	ownerID := teamOwner(t, d, teamID)
	act, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "rank"})
	if err != nil {
		t.Fatal(err)
	}
	popular, err := d.CreateTagForMember(ctx, appmodel.TagCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "popular"})
	if err != nil {
		t.Fatal(err)
	}
	niche, err := d.CreateTagForMember(ctx, appmodel.TagCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "niche"})
	if err != nil {
		t.Fatal(err)
	}
	// popular has 3 sessions, niche has 1.
	for i := 0; i < 3; i++ {
		s, err := d.CreateClosedSession(requestctx.WithActor(ctx, ownerID), appmodel.TimerAddRequest{TeamID: teamID, ActivityID: act.ID, Start: time.Now().Add(-time.Duration(i+2) * time.Hour), End: time.Now().Add(-time.Duration(i+1) * time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		if err := d.AttachTagForMember(ctx, appmodel.SessionTagRequest{TeamID: teamID, CallerID: ownerID, SessionID: s.ID, Name: popular.Name}); err != nil {
			t.Fatal(err)
		}
	}
	one, err := d.CreateClosedSession(requestctx.WithActor(ctx, ownerID), appmodel.TimerAddRequest{TeamID: teamID, ActivityID: act.ID, Start: time.Now().Add(-5 * time.Hour), End: time.Now().Add(-4 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AttachTagForMember(ctx, appmodel.SessionTagRequest{TeamID: teamID, CallerID: ownerID, SessionID: one.ID, Name: niche.Name}); err != nil {
		t.Fatal(err)
	}

	tags, err := d.ListAllTagsWithCounts(ctx, appmodel.TagListQuery{TeamID: teamID})
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) < 2 {
		t.Fatalf("want >=2 tags, got %d", len(tags))
	}
	if tags[0].Name != "popular" || tags[0].SessionCount != 3 {
		t.Errorf("top tag wrong: %+v", tags[0])
	}
	if tags[1].Name != "niche" || tags[1].SessionCount != 1 {
		t.Errorf("second tag wrong: %+v", tags[1])
	}
}
