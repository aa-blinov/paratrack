package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestChangeEmailNeedsTheCurrentCredential(t *testing.T) {
	d := openTestDB(t)
	svc := newTestAuthService(t, d)
	ctx := requestctx.WithActor(context.Background(), 1)
	userID, _, err := svc.createUser(context.Background(), "email-change@example.com", "current-password", "Email Change")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	ctx = requestctx.WithActor(ctx, userID)

	if err := svc.ChangeEmail(ctx, appmodel.ProfileEmailRequest{
		UserID: userID, CallerID: userID, CurrentPassword: "not-the-password", Email: "moved@example.com",
	}); !errors.Is(err, ErrBadPassword) {
		t.Fatalf("ChangeEmail with wrong current password: got %v, want ErrBadPassword", err)
	}
	user, err := svc.findByID(ctx, userID)
	if err != nil || user.Email != "email-change@example.com" {
		t.Fatalf("wrong current password changed the address: user=%+v err=%v", user, err)
	}
}

func TestChangeEmailMovesTheLoginAddress(t *testing.T) {
	d := openTestDB(t)
	svc := newTestAuthService(t, d)
	ctx := context.Background()
	userID, _, err := svc.createUser(ctx, "before@example.com", "current-password", "Email Move")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	ctx = requestctx.WithActor(ctx, userID)

	if err := svc.ChangeEmail(ctx, appmodel.ProfileEmailRequest{
		UserID: userID, CallerID: userID, CurrentPassword: "current-password", Email: "  After@Example.COM ",
	}); err != nil {
		t.Fatalf("ChangeEmail: %v", err)
	}
	user, err := svc.findByID(ctx, userID)
	if err != nil || user.Email != "after@example.com" {
		t.Fatalf("email not normalized and stored: user=%+v err=%v", user, err)
	}
	// The old address must stop working and the new one must start.
	if _, err := svc.findByEmail(ctx, "before@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old address still resolves: %v", err)
	}
	if _, err := svc.findByEmail(ctx, "after@example.com"); err != nil {
		t.Fatalf("new address does not resolve: %v", err)
	}
	var audits int
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE user_id = ? AND action = 'auth.email_change'`, userID,
	).Scan(&audits); err != nil {
		t.Fatalf("count email-change audits: %v", err)
	}
	if audits != 1 {
		t.Fatalf("email-change audits = %d, want 1", audits)
	}
}

func TestChangeEmailRejectsAnAddressAnotherAccountOwns(t *testing.T) {
	d := openTestDB(t)
	svc := newTestAuthService(t, d)
	ctx := context.Background()
	firstID, _, err := svc.createUser(ctx, "first@example.com", "current-password", "First")
	if err != nil {
		t.Fatalf("create first user: %v", err)
	}
	if _, _, err := svc.createUser(ctx, "second@example.com", "current-password", "Second"); err != nil {
		t.Fatalf("create second user: %v", err)
	}
	ctx = requestctx.WithActor(ctx, firstID)

	if err := svc.ChangeEmail(ctx, appmodel.ProfileEmailRequest{
		UserID: firstID, CallerID: firstID, CurrentPassword: "current-password", Email: "second@example.com",
	}); !errors.Is(err, appmodel.ErrAuthEmailTaken) {
		t.Fatalf("colliding address: got %v, want ErrAuthEmailTaken", err)
	}
	user, err := svc.findByID(ctx, firstID)
	if err != nil || user.Email != "first@example.com" {
		t.Fatalf("failed change moved the address anyway: user=%+v err=%v", user, err)
	}
}

func TestChangeEmailRejectsMalformedAddressesAndStrangers(t *testing.T) {
	d := openTestDB(t)
	svc := newTestAuthService(t, d)
	ctx := context.Background()
	userID, _, err := svc.createUser(ctx, "shape@example.com", "current-password", "Shape")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	otherID, _, err := svc.createUser(ctx, "other@example.com", "current-password", "Other")
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}
	ctx = requestctx.WithActor(ctx, userID)

	if err := svc.ChangeEmail(ctx, appmodel.ProfileEmailRequest{
		UserID: userID, CallerID: userID, CurrentPassword: "current-password", Email: "not-an-address",
	}); !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("malformed address: got %v, want ErrInvalidEmail", err)
	}
	if err := svc.ChangeEmail(ctx, appmodel.ProfileEmailRequest{
		UserID: otherID, CallerID: userID, CurrentPassword: "current-password", Email: "hijack@example.com",
	}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("changing somebody else's address: got %v, want ErrForbidden", err)
	}
}

func TestChangeEmailToTheSameAddressIsANoOp(t *testing.T) {
	d := openTestDB(t)
	svc := newTestAuthService(t, d)
	ctx := context.Background()
	userID, _, err := svc.createUser(ctx, "same@example.com", "current-password", "Same")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	ctx = requestctx.WithActor(ctx, userID)

	if err := svc.ChangeEmail(ctx, appmodel.ProfileEmailRequest{
		UserID: userID, CallerID: userID, CurrentPassword: "current-password", Email: "SAME@example.com",
	}); err != nil {
		t.Fatalf("ChangeEmail to the same address: %v", err)
	}
	var audits int
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE user_id = ? AND action = 'auth.email_change'`, userID,
	).Scan(&audits); err != nil {
		t.Fatalf("count email-change audits: %v", err)
	}
	if audits != 0 {
		t.Fatalf("no-op change recorded %d audits, want 0", audits)
	}
}
