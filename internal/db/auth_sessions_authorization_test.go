package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestDeleteAuthSessionsByUserRequiresAuthenticatedOwner(t *testing.T) {
	d, err := OpenTest(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ownerID, _, err := d.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: "sessions-owner@example.com", PasswordHash: "hash", Name: "Owner", TeamName: "Owner workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	otherID, _, err := d.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: "sessions-other@example.com", PasswordHash: "hash", Name: "Other", TeamName: "Other workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	stamp := FormatTime(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC))
	for _, session := range []struct {
		token  string
		userID int64
	}{{"owner-session", ownerID}, {"other-session", otherID}} {
		if _, err := d.TestSQL().ExecContext(ctx,
			`INSERT INTO auth_sessions (token, user_id, created_at, expires_at, last_seen_at) VALUES (?, ?, ?, ?, ?)`,
			session.token, session.userID, stamp, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	request := appmodel.AuthSessionsDeleteByUserRequest{UserID: ownerID, CallerID: ownerID}
	if err := d.DeleteAuthSessionsByUser(ctx, request); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("delete without actor = %v, want forbidden", err)
	}
	if err := d.DeleteAuthSessionsByUser(requestctx.WithActor(ctx, otherID), request); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("delete with another actor = %v, want forbidden", err)
	}
	if err := d.DeleteAuthSessionsByUser(requestctx.WithActor(ctx, ownerID), request); err != nil {
		t.Fatalf("owner delete = %v, want success", err)
	}
	var remaining int
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT count(*) FROM auth_sessions`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("sessions remaining = %d, want the other user's session to remain", remaining)
	}
}
