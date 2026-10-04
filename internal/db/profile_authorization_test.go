package db

import (
	"context"
	"errors"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestProfileWritesRequireAuthenticatedOwner(t *testing.T) {
	d, err := OpenTest(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ownerID, _, err := d.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: "profile-owner@example.com", PasswordHash: "old-hash", Name: "Owner", TeamName: "Owner workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	otherID, _, err := d.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: "profile-other@example.com", PasswordHash: "other-hash", Name: "Other", TeamName: "Other workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	name := appmodel.ProfileNameRequest{UserID: ownerID, CallerID: ownerID, Name: "Changed"}
	if err := d.UpdateUserName(ctx, name); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("name write without actor = %v, want forbidden", err)
	}
	if err := d.UpdateUserName(requestctx.WithActor(ctx, otherID), name); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("name write with another actor = %v, want forbidden", err)
	}
	if err := d.UpdateUserName(requestctx.WithActor(ctx, ownerID), name); err != nil {
		t.Fatalf("owner name write = %v, want success", err)
	}

	password := appmodel.PasswordHashUpdateRequest{
		UserID: ownerID, CallerID: ownerID, ExpectedHash: "old-hash", PasswordHash: "new-hash",
	}
	if _, err := d.UpdateUserPasswordIfHashMatches(ctx, password); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("password write without actor = %v, want forbidden", err)
	}
	if _, err := d.UpdateUserPasswordIfHashMatches(requestctx.WithActor(ctx, otherID), password); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("password write with another actor = %v, want forbidden", err)
	}
	updated, err := d.UpdateUserPasswordIfHashMatches(requestctx.WithActor(ctx, ownerID), password)
	if err != nil || !updated {
		t.Fatalf("owner password write = (%v, %v), want updated", updated, err)
	}
}
