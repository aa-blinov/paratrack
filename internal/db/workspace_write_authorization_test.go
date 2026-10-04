package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

func TestWorkspaceWrites_RecheckManagerRole(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")
	ownerID := teamOwner(t, d, teamID)
	memberID := seedProjectTestUser(t, d, "workspace-member")
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, memberID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}

	if _, err := d.UpsertGoalForManager(ctx, appmodel.GoalUpsertRequest{TeamID: teamID, CallerID: ownerID, ActivityName: "focus", Period: "daily", Minutes: 30}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertGoalForManager(ctx, appmodel.GoalUpsertRequest{TeamID: teamID, CallerID: memberID, ActivityName: "unauthorized", Period: "daily", Minutes: 60}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("member goal upsert error = %v, want forbidden", err)
	}
	if err := d.DeleteGoalForManager(ctx, appmodel.GoalDeleteRequest{TeamID: teamID, CallerID: memberID, ActivityName: "focus", Period: "daily"}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("member goal delete error = %v, want forbidden", err)
	}
	activities, err := d.ListActivities(ctx, teamID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(activities) != 1 || activities[0].Name != "focus" {
		t.Fatalf("unauthorized goal upsert left activity behind: %+v", activities)
	}
	if _, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: memberID, Name: "member activity"}); err != nil {
		t.Fatalf("member activity resolve error = %v, want success", err)
	}

	tag, err := d.CreateTagForMember(ctx, appmodel.TagCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "keep"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteTagForManager(ctx, appmodel.TagDeleteRequest{TeamID: teamID, CallerID: memberID, TagID: tag.ID}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("member tag delete error = %v, want forbidden", err)
	}
	tags, err := d.ListTags(ctx, teamID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 || tags[0].ID != tag.ID {
		t.Fatalf("unauthorized tag delete changed tags: %+v", tags)
	}
}
