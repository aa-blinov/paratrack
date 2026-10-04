package requestctx

import (
	"context"
	"testing"
)

func TestValuesAreIndependentAndInherited(t *testing.T) {
	base := context.Background()
	ctx := WithActor(base, 17)
	ctx = WithScope(ctx, 23)
	ctx = WithClientIP(ctx, "203.0.113.8")
	ctx = WithTeamID(ctx, 41)
	ctx = WithLocale(ctx, "ru")

	if got := ActorID(ctx); got != 17 {
		t.Fatalf("ActorID = %d, want 17", got)
	}
	if got := ScopedUserID(ctx); got != 23 {
		t.Fatalf("ScopedUserID = %d, want 23", got)
	}
	if got := ClientIP(ctx); got != "203.0.113.8" {
		t.Fatalf("ClientIP = %q, want trusted client address", got)
	}
	if got := TeamID(ctx); got != 41 {
		t.Fatalf("TeamID = %d, want 41", got)
	}
	if got := Locale(ctx); got != "ru" {
		t.Fatalf("Locale = %q, want ru", got)
	}
	if ActorID(base) != 0 || TeamID(base) != 0 || ClientIP(base) != "" {
		t.Fatal("attaching request values mutated the parent context")
	}

	child := WithActor(ctx, 19)
	if ActorID(child) != 19 || ScopedUserID(child) != 23 || TeamID(child) != 41 {
		t.Fatal("derived context did not override only its actor value")
	}
}

func TestActorValueRequiresPositiveActor(t *testing.T) {
	for _, id := range []int64{0, -1} {
		if got := ActorValue(WithActor(context.Background(), id)); got != nil {
			t.Fatalf("ActorValue(%d) = %#v, want nil", id, got)
		}
	}
	if got := ActorValue(WithActor(context.Background(), 5)); got != int64(5) {
		t.Fatalf("ActorValue(5) = %#v, want int64(5)", got)
	}
}
