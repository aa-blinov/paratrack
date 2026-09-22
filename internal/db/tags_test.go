package db

import (
	"testing"
	"time"
)

func TestCreateTag_Idempotent(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	a, err := d.CreateTag(ctx, "deep-work")
	if err != nil {
		t.Fatal(err)
	}
	b, err := d.CreateTag(ctx, "deep-work")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID {
		t.Errorf("CreateTag on existing name: a.ID=%d, b.ID=%d (should be equal)", a.ID, b.ID)
	}
}

func TestCreateTag_TrimsWhitespace(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	t1, err := d.CreateTag(ctx, "  spaced  ")
	if err != nil {
		t.Fatal(err)
	}
	t2, err := d.GetTagByName(ctx, "spaced")
	if err != nil {
		t.Fatalf("tag should be retrievable by trimmed name: %v", err)
	}
	if t1.ID != t2.ID {
		t.Errorf("trimmed create should match lookup: %d vs %d", t1.ID, t2.ID)
	}
}

func TestCreateTag_RejectsEmpty(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	if _, err := d.CreateTag(ctx, "   "); err == nil {
		t.Error("CreateTag on whitespace-only should fail, got nil")
	}
}

func TestAttachTag_AutoCreates(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	act, _ := d.GetOrCreateActivity(ctx, "writing")
	s, err := d.CreateSession(ctx, act.ID, time.Now(), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AttachTag(ctx, s.ID, "deep-work"); err != nil {
		t.Fatalf("AttachTag should auto-create: %v", err)
	}
	// Tag should exist now.
	tags, err := d.ListTagsForSession(ctx, s.ID)
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
	act, _ := d.GetOrCreateActivity(ctx, "reading")
	s, _ := d.CreateSession(ctx, act.ID, time.Now(), "")
	_ = d.AttachTag(ctx, s.ID, "morning")
	if err := d.AttachTag(ctx, s.ID, "morning"); err != nil {
		t.Errorf("double attach should be no-op, got: %v", err)
	}
	tags, _ := d.ListTagsForSession(ctx, s.ID)
	if len(tags) != 1 {
		t.Errorf("expected 1 tag after double attach, got %d", len(tags))
	}
}

func TestDetachTag_KeepsTagAlive(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	act, _ := d.GetOrCreateActivity(ctx, "work")
	s, _ := d.CreateSession(ctx, act.ID, time.Now(), "")
	_, _ = d.CreateTag(ctx, "office")
	if err := d.AttachTag(ctx, s.ID, "office"); err != nil {
		t.Fatal(err)
	}
	if err := d.DetachTag(ctx, s.ID, "office"); err != nil {
		t.Fatal(err)
	}
	tags, _ := d.ListTagsForSession(ctx, s.ID)
	if len(tags) != 0 {
		t.Errorf("session still has tags after detach: %v", tags)
	}
	// Tag itself should still exist.
	all, _ := d.ListTags(ctx)
	found := false
	for _, tg := range all {
		if tg.Name == "office" {
			found = true
		}
	}
	if !found {
		t.Error("detach removed the tag from the catalogue, expected it to remain")
	}
}

func TestSetTagsForSession_ReplacesAll(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	act, _ := d.GetOrCreateActivity(ctx, "study")
	s, _ := d.CreateSession(ctx, act.ID, time.Now(), "")
	_ = d.AttachTag(ctx, s.ID, "old1")
	_ = d.AttachTag(ctx, s.ID, "old2")

	if err := d.SetTagsForSession(ctx, s.ID, []string{"new1", "new2", "new3"}); err != nil {
		t.Fatal(err)
	}
	tags, _ := d.ListTagsForSession(ctx, s.ID)
	got := map[string]bool{}
	for _, tg := range tags {
		got[tg.Name] = true
	}
	if len(got) != 3 || !got["new1"] || !got["new2"] || !got["new3"] {
		t.Errorf("after replace, tags = %v; want only new1/new2/new3", tags)
	}
	if got["old1"] || got["old2"] {
		t.Error("SetTagsForSession should have removed old tags")
	}
}

func TestSetTagsForSession_EmptyClears(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	act, _ := d.GetOrCreateActivity(ctx, "calm")
	s, _ := d.CreateSession(ctx, act.ID, time.Now(), "")
	_ = d.AttachTag(ctx, s.ID, "temp")
	if err := d.SetTagsForSession(ctx, s.ID, nil); err != nil {
		t.Fatal(err)
	}
	tags, _ := d.ListTagsForSession(ctx, s.ID)
	if len(tags) != 0 {
		t.Errorf("empty set should clear all tags, got %v", tags)
	}
}

func TestTagsForSessions_BatchedAcrossMany(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	act, _ := d.GetOrCreateActivity(ctx, "batch")
	var sids []int64
	for i := 0; i < 5; i++ {
		s, _ := d.CreateSession(ctx, act.ID, time.Now(), "")
		sids = append(sids, s.ID)
	}
	// Tag session 0 with 'a', 1 with 'b', 2 with both.
	_ = d.AttachTag(ctx, sids[0], "a")
	_ = d.AttachTag(ctx, sids[1], "b")
	_ = d.AttachTag(ctx, sids[2], "a")
	_ = d.AttachTag(ctx, sids[2], "b")

	got, err := d.TagsForSessions(ctx, sids)
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
	act, _ := d.GetOrCreateActivity(ctx, "doomed")
	s, _ := d.CreateSession(ctx, act.ID, time.Now(), "")
	tag, _ := d.CreateTag(ctx, "doomed")
	if err := d.AttachTag(ctx, s.ID, "doomed"); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteTag(ctx, tag.ID); err != nil {
		t.Fatal(err)
	}
	tags, _ := d.ListTagsForSession(ctx, s.ID)
	if len(tags) != 0 {
		t.Errorf("session_tags should cascade: got %v", tags)
	}
}

func TestListAllTagsWithCounts_OrderedByPopularity(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	act, _ := d.GetOrCreateActivity(ctx, "rank")
	// popular has 3 sessions, niche has 1.
	for i := 0; i < 3; i++ {
		s, _ := d.CreateSession(ctx, act.ID, time.Now(), "")
		_ = d.AttachTag(ctx, s.ID, "popular")
	}
	one, _ := d.CreateSession(ctx, act.ID, time.Now(), "")
	_ = d.AttachTag(ctx, one.ID, "niche")

	tags, err := d.ListAllTagsWithCounts(ctx)
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