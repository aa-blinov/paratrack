package db

import (
	"context"
	"testing"
	"time"
)

func TestCreateTag_Idempotent(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	a, err := d.CreateTag(ctx, 0, "deep-work")
	if err != nil {
		t.Fatal(err)
	}
	b, err := d.CreateTag(ctx, 0, "deep-work")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID {
		t.Errorf("CreateTag on existing name: a.ID=%d, b.ID=%d (should be equal)", a.ID, b.ID)
	}
	// Mixed-case input must collapse to the same tag — the COLLATE
	// NOCASE column guarantees it but CreateTag also normalises on the
	// way in so the *output* name is the canonical lowercase.
	c, err := d.CreateTag(ctx, 0, "Deep-Work")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != c.ID || c.Name != "deep-work" {
		t.Errorf("mixed-case 'Deep-Work' should resolve to %d (name=%q), got %d (name=%q)",
			a.ID, "deep-work", c.ID, c.Name)
	}
}

func TestGetTagByName_IsCaseInsensitive(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	a, err := d.CreateTag(ctx, 0, "morning")
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{"MORNING", "Morning", "mOrNiNg"} {
		got, err := d.GetTagByName(ctx, 0, in)
		if err != nil {
			t.Errorf("GetTagByName(%q): %v", in, err)
			continue
		}
		if got.ID != a.ID || got.Name != "morning" {
			t.Errorf("GetTagByName(%q) = %d (%q), want %d (morning)", in, got.ID, got.Name, a.ID)
		}
	}
}

func TestCreateTag_TrimsWhitespace(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	t1, err := d.CreateTag(ctx, 0, "  spaced  ")
	if err != nil {
		t.Fatal(err)
	}
	t2, err := d.GetTagByName(ctx, 0, "spaced")
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
	if _, err := d.CreateTag(ctx, 0, "   "); err == nil {
		t.Error("CreateTag on whitespace-only should fail, got nil")
	}
}

func TestAttachTag_AutoCreates(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	act, _ := d.GetOrCreateActivity(ctx, 0, "writing")
	s, err := d.CreateSession(ctx, 0, act.ID, time.Now(), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AttachTag(ctx, 0, s.ID, "deep-work"); err != nil {
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
	act, _ := d.GetOrCreateActivity(ctx, 0, "reading")
	s, _ := d.CreateSession(ctx, 0, act.ID, time.Now(), "")
	_ = d.AttachTag(ctx, 0, s.ID, "morning")
	if err := d.AttachTag(ctx, 0, s.ID, "morning"); err != nil {
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
	act, _ := d.GetOrCreateActivity(ctx, 0, "work")
	s, _ := d.CreateSession(ctx, 0, act.ID, time.Now(), "")
	_, _ = d.CreateTag(ctx, 0, "office")
	if err := d.AttachTag(ctx, 0, s.ID, "office"); err != nil {
		t.Fatal(err)
	}
	if err := d.DetachTag(ctx, 0, s.ID, "office"); err != nil {
		t.Fatal(err)
	}
	tags, _ := d.ListTagsForSession(ctx, s.ID)
	if len(tags) != 0 {
		t.Errorf("session still has tags after detach: %v", tags)
	}
	// Tag itself should still exist.
	all, _ := d.ListTags(ctx, 0)
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
	act, _ := d.GetOrCreateActivity(ctx, 0, "study")
	s, _ := d.CreateSession(ctx, 0, act.ID, time.Now(), "")
	_ = d.AttachTag(ctx, 0, s.ID, "old1")
	_ = d.AttachTag(ctx, 0, s.ID, "old2")

	if err := d.SetTagsForSession(ctx, 0, s.ID, []string{"new1", "new2", "new3"}); err != nil {
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
	act, _ := d.GetOrCreateActivity(ctx, 0, "calm")
	s, _ := d.CreateSession(ctx, 0, act.ID, time.Now(), "")
	_ = d.AttachTag(ctx, 0, s.ID, "temp")
	if err := d.SetTagsForSession(ctx, 0, s.ID, nil); err != nil {
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
	act, _ := d.GetOrCreateActivity(ctx, 0, "batch")
	var sids []int64
	for i := 0; i < 5; i++ {
		s, _ := d.CreateSession(ctx, 0, act.ID, time.Now(), "")
		sids = append(sids, s.ID)
	}
	// Tag session 0 with 'a', 1 with 'b', 2 with both.
	_ = d.AttachTag(ctx, 0, sids[0], "a")
	_ = d.AttachTag(ctx, 0, sids[1], "b")
	_ = d.AttachTag(ctx, 0, sids[2], "a")
	_ = d.AttachTag(ctx, 0, sids[2], "b")

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
	act, _ := d.GetOrCreateActivity(ctx, 0, "doomed")
	s, _ := d.CreateSession(ctx, 0, act.ID, time.Now(), "")
	tag, _ := d.CreateTag(ctx, 0, "doomed")
	if err := d.AttachTag(ctx, 0, s.ID, "doomed"); err != nil {
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
	act, _ := d.GetOrCreateActivity(ctx, 0, "rank")
	// popular has 3 sessions, niche has 1.
	for i := 0; i < 3; i++ {
		s, _ := d.CreateSession(ctx, 0, act.ID, time.Now(), "")
		_ = d.AttachTag(ctx, 0, s.ID, "popular")
	}
	one, _ := d.CreateSession(ctx, 0, act.ID, time.Now(), "")
	_ = d.AttachTag(ctx, 0, one.ID, "niche")

	tags, err := d.ListAllTagsWithCounts(ctx, 0)
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

// TestNormalizeTagCase_MergesDuplicates mirrors the activities test
// for tags: case-variant duplicates must collapse to one row, and
// session_tags from the loser have to be re-pointed at the winner.
func TestNormalizeTagCase_MergesDuplicates(t *testing.T) {
	d := openLegacySchemaDB(t)
	ctx := t.Context()

	act, err := d.GetOrCreateActivity(ctx, 0, "work")
	if err != nil {
		t.Fatal(err)
	}

	winnerID, err := insertRawTag(ctx, d, "Morning")
	if err != nil {
		t.Fatal(err)
	}
	loserID, err := insertRawTag(ctx, d, "MORNING")
	if err != nil {
		t.Fatal(err)
	}
	if winnerID == loserID {
		t.Fatal("raw inserts unexpectedly collided on the same row")
	}
	keepID, err := insertRawTag(ctx, d, "evening")
	if err != nil {
		t.Fatal(err)
	}

	// Session 1 has winner only, session 2 has loser only, session 3 has both.
	s1, _ := d.CreateSession(ctx, 0, act.ID, time.Now(), "")
	s2, _ := d.CreateSession(ctx, 0, act.ID, time.Now(), "")
	s3, _ := d.CreateSession(ctx, 0, act.ID, time.Now(), "")
	if _, err := d.sql.ExecContext(ctx,
		`INSERT INTO session_tags (session_id, tag_id) VALUES (?, ?), (?, ?), (?, ?)`,
		s1.ID, winnerID, s2.ID, loserID, s3.ID, loserID,
	); err != nil {
		t.Fatal(err)
	}

	if err := d.normalizeTagCase(); err != nil {
		t.Fatalf("normalizeTagCase: %v", err)
	}

	// Loser gone, winner lowercased.
	var loserFound int
	if err := d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM tags WHERE id = ?`, loserID).Scan(&loserFound); err != nil {
		t.Fatal(err)
	}
	if loserFound != 0 {
		t.Errorf("loser tag id=%d should be deleted, still present", loserID)
	}
	var winnerName string
	if err := d.sql.QueryRowContext(ctx, `SELECT name FROM tags WHERE id = ?`, winnerID).Scan(&winnerName); err != nil {
		t.Fatalf("winner vanished: %v", err)
	}
	if winnerName != "morning" {
		t.Errorf("winner name = %q, want %q", winnerName, "morning")
	}
	// Loser-only session 2 should now have the winner tag via merge.
	tagsOn2, err := d.ListTagsForSession(ctx, s2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tagsOn2) != 1 || tagsOn2[0].ID != winnerID {
		t.Errorf("session 2 tags = %+v, want only winner (%d)", tagsOn2, winnerID)
	}
	// Session 3 already had the winner — OR IGNORE should keep it as a single row.
	tagsOn3, err := d.ListTagsForSession(ctx, s3.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tagsOn3) != 1 || tagsOn3[0].ID != winnerID {
		t.Errorf("session 3 tags = %+v, want exactly winner (%d)", tagsOn3, winnerID)
	}
	// Untouched tag and its session still intact.
	tagsOn1, err := d.ListTagsForSession(ctx, s1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tagsOn1) != 1 || tagsOn1[0].ID != winnerID {
		t.Errorf("session 1 tags = %+v", tagsOn1)
	}
	var keepFound int
if err := d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM tags WHERE id = ?`, keepID).Scan(&keepFound); err != nil {
		t.Fatal(err)
	}
if keepFound != 1 {
		t.Errorf("untouched tag id=%d disappeared", keepID)
}

	// Idempotent — second run is a no-op.
	if err := d.normalizeTagCase(); err != nil {
		t.Fatalf("second normalizeTagCase: %v", err)
	}
	all, err := d.ListTags(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("after merge expected 2 tags, got %d: %+v", len(all), all)
	}
}

// insertRawTag plants a tag row with whatever casing the caller
// specifies. Like insertRawActivity, it only works against the
// legacy schema produced by openLegacySchemaDB.
func insertRawTag(ctx context.Context, d *DB, name string) (int64, error) {
	res, err := d.sql.ExecContext(ctx,
		`INSERT INTO tags (name) VALUES (?)`, name,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}