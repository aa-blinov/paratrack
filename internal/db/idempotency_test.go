package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

func TestIdempotencyKeyClaimsFinishesAndReleases(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	start := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

	done, _, err := d.ClaimIdempotencyKey(ctx, appmodel.IdempotencyRequest{UserID: 7, Key: "k-1", Now: start})
	if err != nil || done {
		t.Fatalf("first claim = done %v, err %v; want a fresh claim", done, err)
	}
	if _, _, err := d.ClaimIdempotencyKey(ctx, appmodel.IdempotencyRequest{UserID: 7, Key: "k-1", Now: start.Add(time.Second)}); !errors.Is(err, model.ErrRequestInProgress) {
		t.Fatalf("second claim while in progress = %v, want ErrRequestInProgress", err)
	}
	if err := d.FinishIdempotencyKey(ctx, appmodel.IdempotencyRequest{UserID: 7, Key: "k-1", Status: 204}); err != nil {
		t.Fatal(err)
	}
	done, status, err := d.ClaimIdempotencyKey(ctx, appmodel.IdempotencyRequest{UserID: 7, Key: "k-1", Now: start.Add(time.Hour)})
	if err != nil || !done || status != 204 {
		t.Fatalf("claim after finish = done %v status %d err %v; want done 204", done, status, err)
	}

	// Another user's key with the same text is a different key.
	if done, _, err := d.ClaimIdempotencyKey(ctx, appmodel.IdempotencyRequest{UserID: 8, Key: "k-1", Now: start}); err != nil || done {
		t.Fatalf("other user's claim = done %v err %v; want a fresh claim", done, err)
	}

	// A released claim is free again.
	if _, _, err := d.ClaimIdempotencyKey(ctx, appmodel.IdempotencyRequest{UserID: 7, Key: "k-2", Now: start}); err != nil {
		t.Fatal(err)
	}
	if err := d.ReleaseIdempotencyKey(ctx, appmodel.IdempotencyRequest{UserID: 7, Key: "k-2"}); err != nil {
		t.Fatal(err)
	}
	if done, _, err := d.ClaimIdempotencyKey(ctx, appmodel.IdempotencyRequest{UserID: 7, Key: "k-2", Now: start}); err != nil || done {
		t.Fatalf("claim after release = done %v err %v; want a fresh claim", done, err)
	}
}

// An attempt that never finished is taken over once its claim is stale.
func TestStaleIdempotencyClaimIsTakenOver(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	start := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

	if _, _, err := d.ClaimIdempotencyKey(ctx, appmodel.IdempotencyRequest{UserID: 7, Key: "k-stale", Now: start}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.ClaimIdempotencyKey(ctx, appmodel.IdempotencyRequest{UserID: 7, Key: "k-stale", Now: start.Add(time.Minute)}); !errors.Is(err, model.ErrRequestInProgress) {
		t.Fatalf("claim inside the stale window = %v, want ErrRequestInProgress", err)
	}
	if done, _, err := d.ClaimIdempotencyKey(ctx, appmodel.IdempotencyRequest{UserID: 7, Key: "k-stale", Now: start.Add(idempotencyStaleAfter + time.Second)}); err != nil || done {
		t.Fatalf("takeover of a stale claim = done %v err %v; want a fresh claim", done, err)
	}
}
