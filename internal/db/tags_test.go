package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// taggedSession seeds one workspace activity and one closed session to hang
// tags on.
func taggedSession(t *testing.T, d *DB, teamID, ownerID int64, activityName string) int64 {
	t.Helper()
	ctx := t.Context()
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: activityName})
	if err != nil {
		t.Fatalf("resolve activity %q: %v", activityName, err)
	}
	start := time.Now().Add(-2 * time.Hour)
	session, err := d.CreateClosedSession(requestctx.WithActor(ctx, ownerID), appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activity.ID, Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return session.ID
}

func TestCreateTagForMember_Idempotent(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Tag create", "tag-create")
	ownerID := teamOwner(t, d, teamID)
	a, err := d.CreateTagForMember(ctx, appmodel.TagCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "deep-work"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := d.CreateTagForMember(ctx, appmodel.TagCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "deep-work"})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID {
		t.Errorf("CreateTagForMember on existing name: a.ID=%d, b.ID=%d (should be equal)", a.ID, b.ID)
	}
	// Mixed-case input must collapse to the same tag: the create path
	// normalises on the way in so the *output* name is the canonical
	// lowercase.
	c, err := d.CreateTagForMember(ctx, appmodel.TagCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Deep-Work"})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != c.ID || c.Name != "deep-work" {
		t.Errorf("mixed-case 'Deep-Work' should resolve to %d (name=%q), got %d (name=%q)",
			a.ID, "deep-work", c.ID, c.Name)
	}
}

func TestCreateTagForMember_TrimsWhitespace(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Tag trim", "tag-trim")
	ownerID := teamOwner(t, d, teamID)
	t1, err := d.CreateTagForMember(ctx, appmodel.TagCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "  spaced  "})
	if err != nil {
		t.Fatal(err)
	}
	if t1.Name != "spaced" {
		t.Errorf("created tag name = %q, want normalized name %q", t1.Name, "spaced")
	}
}

func TestCreateTagForMember_RejectsEmpty(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Tag empty", "tag-empty")
	ownerID := teamOwner(t, d, teamID)
	if _, err := d.CreateTagForMember(ctx, appmodel.TagCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "   "}); err == nil {
		t.Error("CreateTagForMember on whitespace-only should fail, got nil")
	}
}

func TestAttachTagForMember_AutoCreates(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Tag attach", "tag-attach")
	ownerID := teamOwner(t, d, teamID)
	sessionID := taggedSession(t, d, teamID, ownerID, "writing")
	if err := d.AttachTagForMember(ctx, appmodel.SessionTagRequest{TeamID: teamID, CallerID: ownerID, SessionID: sessionID, Name: "deep-work"}); err != nil {
		t.Fatalf("AttachTagForMember should auto-create: %v", err)
	}
	tags, err := d.TagsForSessions(ctx, appmodel.SessionTagsQuery{TeamID: teamID, SessionIDs: []int64{sessionID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tags[sessionID]) != 1 || tags[sessionID][0].Name != "deep-work" {
		t.Errorf("attached tag wrong: %+v", tags[sessionID])
	}
}

func TestAttachTagForMember_Idempotent(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Tag reattach", "tag-reattach")
	ownerID := teamOwner(t, d, teamID)
	sessionID := taggedSession(t, d, teamID, ownerID, "reading")
	request := appmodel.SessionTagRequest{TeamID: teamID, CallerID: ownerID, SessionID: sessionID, Name: "morning"}
	if err := d.AttachTagForMember(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := d.AttachTagForMember(ctx, request); err != nil {
		t.Errorf("double attach should be no-op, got: %v", err)
	}
	tags, err := d.TagsForSessions(ctx, appmodel.SessionTagsQuery{TeamID: teamID, SessionIDs: []int64{sessionID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tags[sessionID]) != 1 {
		t.Errorf("expected 1 tag after double attach, got %d", len(tags[sessionID]))
	}
}

func TestDetachTagForMember_KeepsTagAlive(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Tag detach", "tag-detach")
	ownerID := teamOwner(t, d, teamID)
	sessionID := taggedSession(t, d, teamID, ownerID, "work")
	if _, err := d.CreateTagForMember(ctx, appmodel.TagCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "office"}); err != nil {
		t.Fatal(err)
	}
	request := appmodel.SessionTagRequest{TeamID: teamID, CallerID: ownerID, SessionID: sessionID, Name: "office"}
	if err := d.AttachTagForMember(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := d.DetachTagForMember(ctx, request); err != nil {
		t.Fatal(err)
	}
	tags, err := d.TagsForSessions(ctx, appmodel.SessionTagsQuery{TeamID: teamID, SessionIDs: []int64{sessionID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tags[sessionID]) != 0 {
		t.Errorf("session still has tags after detach: %v", tags[sessionID])
	}
	// Tag itself should still exist.
	var count int
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM tags WHERE team_id = ? AND name = 'office'`, teamID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Error("detach removed the tag from the catalogue, expected it to remain")
	}
}

func TestTagsForSessions_BatchedAcrossMany(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Tag batch", "tag-batch")
	ownerID := teamOwner(t, d, teamID)
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: ownerID, Name: "batch"})
	if err != nil {
		t.Fatal(err)
	}
	var sids []int64
	for i := 0; i < 5; i++ {
		start := time.Now().Add(-time.Duration(i+2) * time.Hour)
		s, err := d.CreateClosedSession(requestctx.WithActor(ctx, ownerID), appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activity.ID, Start: start, End: start.Add(time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		sids = append(sids, s.ID)
	}
	attach := func(sessionID int64, name string) {
		t.Helper()
		if err := d.AttachTagForMember(ctx, appmodel.SessionTagRequest{TeamID: teamID, CallerID: ownerID, SessionID: sessionID, Name: name}); err != nil {
			t.Fatalf("attach %q: %v", name, err)
		}
	}
	// Tag session 0 with 'a', 1 with 'b', 2 with both.
	attach(sids[0], "a")
	attach(sids[1], "b")
	attach(sids[2], "a")
	attach(sids[2], "b")

	got, err := d.TagsForSessions(ctx, appmodel.SessionTagsQuery{TeamID: teamID, SessionIDs: sids})
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

func TestDeleteTagForManager_CascadesIntoSessionTags(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Tag cascade", "tag-cascade")
	ownerID := teamOwner(t, d, teamID)
	sessionID := taggedSession(t, d, teamID, ownerID, "doomed")
	tag, err := d.CreateTagForMember(ctx, appmodel.TagCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "doomed"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AttachTagForMember(ctx, appmodel.SessionTagRequest{TeamID: teamID, CallerID: ownerID, SessionID: sessionID, Name: "doomed"}); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteTagForManager(ctx, appmodel.TagDeleteRequest{TeamID: teamID, CallerID: ownerID, TagID: tag.ID}); err != nil {
		t.Fatal(err)
	}
	tags, err := d.TagsForSessions(ctx, appmodel.SessionTagsQuery{TeamID: teamID, SessionIDs: []int64{sessionID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tags[sessionID]) != 0 {
		t.Errorf("session_tags should cascade: got %v", tags[sessionID])
	}
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
