package db

import (
	"context"
	"errors"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestPushSubscriptionMutationsRequireAuthenticatedOwner(t *testing.T) {
	d, err := OpenTest(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ownerID, teamID, err := d.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: "push-owner@example.com", PasswordHash: "hash", Name: "Owner", TeamName: "Owner workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	otherID, _, err := d.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: "push-other@example.com", PasswordHash: "hash", Name: "Other", TeamName: "Other workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	subscription := appmodel.PushSubscribeRequest{
		TeamID: teamID, UserID: ownerID, CallerID: ownerID,
		Endpoint: "https://push.example/device", PublicKey: "public", AuthSecret: "auth",
	}
	if err := d.UpsertPushSubscription(ctx, subscription); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("subscribe without request actor = %v, want forbidden", err)
	}
	if err := d.UpsertPushSubscription(requestctx.WithActor(ctx, otherID), subscription); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("subscribe with another actor = %v, want forbidden", err)
	}
	ownerCtx := requestctx.WithActor(ctx, ownerID)
	if err := d.UpsertPushSubscription(ownerCtx, subscription); err != nil {
		t.Fatalf("owner subscribe = %v, want success", err)
	}
	unsubscribe := appmodel.PushUnsubscribeRequest{
		TeamID: teamID, UserID: ownerID, CallerID: ownerID, Endpoint: subscription.Endpoint,
	}
	if err := d.DeletePushSubscription(requestctx.WithActor(ctx, otherID), unsubscribe); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("unsubscribe with another actor = %v, want forbidden", err)
	}
	if err := d.DeletePushSubscription(ownerCtx, unsubscribe); err != nil {
		t.Fatalf("owner unsubscribe = %v, want success", err)
	}
}
