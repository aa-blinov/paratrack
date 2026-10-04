package db

import (
	"context"
	"errors"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestAPITokenManagementRequiresAuthenticatedOwner(t *testing.T) {
	d, err := OpenTest(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ownerID, _, err := d.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: "api-token-owner@example.com", PasswordHash: "hash", Name: "Owner", TeamName: "Owner workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	otherID, _, err := d.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: "api-token-other@example.com", PasswordHash: "hash", Name: "Other", TeamName: "Other workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	create := appmodel.APITokenCreateRequest{UserID: ownerID, CallerID: ownerID, Name: "owner token"}
	if _, _, err := d.CreateAPIToken(ctx, create); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("create without request actor = %v, want forbidden", err)
	}
	if _, _, err := d.CreateAPIToken(requestctx.WithActor(ctx, otherID), create); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("create with another actor = %v, want forbidden", err)
	}
	ownerCtx := requestctx.WithActor(ctx, ownerID)
	_, token, err := d.CreateAPIToken(ownerCtx, create)
	if err != nil {
		t.Fatal(err)
	}
	list := appmodel.APITokenListRequest{UserID: ownerID, CallerID: ownerID}
	if _, err := d.ListAPITokens(ctx, list); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("list without request actor = %v, want forbidden", err)
	}
	if _, err := d.ListAPITokens(requestctx.WithActor(ctx, otherID), list); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("list with another actor = %v, want forbidden", err)
	}
	if tokens, err := d.ListAPITokens(ownerCtx, list); err != nil || len(tokens) != 1 {
		t.Fatalf("owner list = (%d tokens, %v), want one token", len(tokens), err)
	}
	remove := appmodel.APITokenDeleteRequest{UserID: ownerID, CallerID: ownerID, TokenID: token.ID}
	if err := d.DeleteAPIToken(requestctx.WithActor(ctx, otherID), remove); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("delete with another actor = %v, want forbidden", err)
	}
	if err := d.DeleteAPIToken(ownerCtx, remove); err != nil {
		t.Fatalf("owner delete = %v, want success", err)
	}
}
