package db

import (
	"context"
	"errors"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// notificationFixture is a workspace with two members, one subscribed device
// each, so topic selection can be read from the point of view of a delivery.
type notificationFixture struct {
	db     *DB
	owner  int64
	member int64
	team   int64
	ctx    context.Context
}

func newNotificationFixture(t *testing.T) notificationFixture {
	t.Helper()
	d, err := OpenTest(t)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ctx := context.Background()
	ownerID, teamID, err := d.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: "notify-owner@example.com", PasswordHash: "hash", Name: "Owner", TeamName: "Notify workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := d.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: "notify-member@example.com", PasswordHash: "hash", Name: "Member", TeamName: "Member workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.sql.ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role) VALUES (?, ?, 'member')`, teamID, second); err != nil {
		t.Fatal(err)
	}
	for i, userID := range []int64{ownerID, second} {
		if err := d.UpsertPushSubscription(requestctx.WithActor(ctx, userID), appmodel.PushSubscribeRequest{
			TeamID: teamID, UserID: userID, CallerID: userID,
			Endpoint: "https://push.example/device", PublicKey: "key", AuthSecret: "auth",
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := d.sql.ExecContext(ctx,
			`UPDATE push_subscriptions SET endpoint = ? WHERE team_id = ? AND user_id = ?`,
			"https://push.example/device"+string(rune('a'+i)), teamID, userID); err != nil {
			t.Fatal(err)
		}
	}
	return notificationFixture{db: d, owner: ownerID, member: second, team: teamID, ctx: requestctx.WithActor(ctx, ownerID)}
}

func TestNotificationTopicsStartOnForAnAccountThatNeverChoseThem(t *testing.T) {
	f := newNotificationFixture(t)

	muted, err := f.db.MutedNotificationTopics(f.ctx, appmodel.NotificationTopicsQuery{TeamID: f.team, UserID: f.owner})
	if err != nil {
		t.Fatal(err)
	}
	if len(muted) != 0 {
		t.Fatalf("muted = %v, want every topic still on", muted)
	}
	targets, err := f.db.NotificationTargets(f.ctx, appmodel.NotificationTargetsQuery{
		TeamID: f.team, UserIDs: []int64{f.owner, f.member}, Topic: appmodel.NotificationTopicPayrollPaid,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 {
		t.Fatalf("targets = %v, want both subscribed members", targets)
	}
}

func TestMutingATopicRemovesOnlyThatPersonFromThatDelivery(t *testing.T) {
	f := newNotificationFixture(t)

	if err := f.db.SetMutedNotificationTopics(f.ctx, appmodel.NotificationTopicsCommand{
		TeamID: f.team, UserID: f.owner, CallerID: f.owner,
		Topics: []string{appmodel.NotificationTopicPayrollPaid},
	}); err != nil {
		t.Fatal(err)
	}
	muted, err := f.db.MutedNotificationTopics(f.ctx, appmodel.NotificationTopicsQuery{TeamID: f.team, UserID: f.owner})
	if err != nil {
		t.Fatal(err)
	}
	if len(muted) != 1 || muted[0] != appmodel.NotificationTopicPayrollPaid {
		t.Fatalf("muted = %v, want only the payroll topic", muted)
	}
	targets, err := f.db.NotificationTargets(f.ctx, appmodel.NotificationTargetsQuery{
		TeamID: f.team, UserIDs: []int64{f.owner, f.member}, Topic: appmodel.NotificationTopicPayrollPaid,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0] != f.member {
		t.Fatalf("targets = %v, want only the member who kept the topic", targets)
	}
	// Another event still reaches both.
	targets, err = f.db.NotificationTargets(f.ctx, appmodel.NotificationTargetsQuery{
		TeamID: f.team, UserIDs: []int64{f.owner, f.member}, Topic: appmodel.NotificationTopicSessionStopped,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 {
		t.Fatalf("targets = %v, want both members for an unmuted topic", targets)
	}
	// Clearing the selection restores every event.
	if err := f.db.SetMutedNotificationTopics(f.ctx, appmodel.NotificationTopicsCommand{
		TeamID: f.team, UserID: f.owner, CallerID: f.owner,
	}); err != nil {
		t.Fatal(err)
	}
	targets, err = f.db.NotificationTargets(f.ctx, appmodel.NotificationTargetsQuery{
		TeamID: f.team, UserIDs: []int64{f.owner}, Topic: appmodel.NotificationTopicPayrollPaid,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets = %v after clearing the selection", targets)
	}
}

func TestMutingAnEventTheAppNeverNotifiesAboutIsRefused(t *testing.T) {
	f := newNotificationFixture(t)

	err := f.db.SetMutedNotificationTopics(f.ctx, appmodel.NotificationTopicsCommand{
		TeamID: f.team, UserID: f.owner, CallerID: f.owner, Topics: []string{"invoice.paid"},
	})
	if !errors.Is(err, appmodel.ErrInvalidNotificationTopic) {
		t.Fatalf("mute of an unsent event = %v, want refused", err)
	}
}

func TestMutedTopicsArePersonalAndRequireTheAuthenticatedOwner(t *testing.T) {
	f := newNotificationFixture(t)
	command := appmodel.NotificationTopicsCommand{
		TeamID: f.team, UserID: f.owner, CallerID: f.owner, Topics: []string{appmodel.NotificationTopicGoalAchieved},
	}
	if err := f.db.SetMutedNotificationTopics(context.Background(), command); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("write without a request actor = %v, want forbidden", err)
	}
	other := requestctx.WithActor(f.ctx, f.member)
	if err := f.db.SetMutedNotificationTopics(other, command); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("write by another member = %v, want forbidden", err)
	}
	command.CallerID = f.member
	if err := f.db.SetMutedNotificationTopics(other, command); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("write to another person's row = %v, want forbidden", err)
	}
	command.CallerID = f.owner
	if err := f.db.SetMutedNotificationTopics(f.ctx, command); err != nil {
		t.Fatalf("own selection = %v, want success", err)
	}
	if _, err := f.db.MutedNotificationTopics(f.ctx, appmodel.NotificationTopicsQuery{TeamID: f.team, UserID: f.owner}); err != nil {
		t.Fatal(err)
	}
	// The other member is untouched by somebody else's choice.
	muted, err := f.db.MutedNotificationTopics(f.ctx, appmodel.NotificationTopicsQuery{TeamID: f.team, UserID: f.member})
	if err != nil {
		t.Fatal(err)
	}
	if len(muted) != 0 {
		t.Fatalf("other member muted = %v, want nothing", muted)
	}
}
