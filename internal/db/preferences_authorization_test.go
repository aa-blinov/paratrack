package db

import (
	"context"
	"errors"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestUserPreferencesWritesRequireTheAuthenticatedOwner(t *testing.T) {
	d, err := OpenTest(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ownerID, _, err := d.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: "preferences-owner@example.com", PasswordHash: "hash", Name: "Owner", TeamName: "Owner workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	otherID, _, err := d.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: "preferences-other@example.com", PasswordHash: "hash", Name: "Other", TeamName: "Other workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	command := appmodel.UserPrefsSaveCommand{UserID: ownerID, CallerID: ownerID, JSON: `{"week_start":"monday"}`}
	if err := d.SetUserPrefs(ctx, command); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("write without request actor = %v, want forbidden", err)
	}
	if err := d.SetUserPrefs(requestctx.WithActor(ctx, otherID), command); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("write with another request actor = %v, want forbidden", err)
	}
	command.CallerID = otherID
	if err := d.SetUserPrefs(requestctx.WithActor(ctx, otherID), command); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("write to another user's row = %v, want forbidden", err)
	}
	command.CallerID = ownerID
	if err := d.SetUserPrefs(requestctx.WithActor(ctx, ownerID), command); err != nil {
		t.Fatalf("owner preference write = %v, want success", err)
	}
}
