package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

func TestTeamTagCountsIgnoreForeignSessionAssociations(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamA := seedTeam(t, d, "Tag owner", "tag-owner")
	ownerA := teamOwner(t, d, teamA)
	teamB := seedTeam(t, d, "Session owner", "tag-session-owner")
	ownerB := teamOwner(t, d, teamB)
	tag, err := d.CreateTagForMember(ctx, appmodel.TagCreateRequest{TeamID: teamA, CallerID: ownerA, Name: "internal"})
	if err != nil {
		t.Fatal(err)
	}
	activityA, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamA, CallerID: ownerA, Name: "team a work"})
	if err != nil {
		t.Fatal(err)
	}
	activityB, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamB, CallerID: ownerB, Name: "team b work"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	sessionA, err := d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: teamA, ActivityID: activityA.ID, Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	sessionB, err := d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: teamB, ActivityID: activityB.ID, Start: start, End: start.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AttachTagForMember(ctx, appmodel.SessionTagRequest{TeamID: teamA, CallerID: ownerA, SessionID: sessionA.ID, Name: tag.Name}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO session_tags (session_id, tag_id) VALUES (?, ?)`, sessionB.ID, tag.ID); err != nil {
		t.Fatal(err)
	}

	tags, err := d.ListAllTagsWithCounts(ctx, teamA)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 || tags[0].SessionCount != 1 {
		t.Fatalf("team A tag counts = %+v, want one tag attached to one team A session", tags)
	}
}

func TestTeamTagReadsRejectMissingWorkspaceScope(t *testing.T) {
	var d *DB
	if _, err := d.ListTags(t.Context(), 0); err != ErrNotFound {
		t.Fatalf("ListTags error = %v, want not found", err)
	}
	if _, err := d.ListAllTagsWithCounts(t.Context(), 0); err != ErrNotFound {
		t.Fatalf("ListAllTagsWithCounts error = %v, want not found", err)
	}
}
