package db

import (
	"context"
	"strings"
	"testing"
)

func TestSecretsSealedAtRest(t *testing.T) {
	t.Setenv("PARATRACK_SECRET_KEY", "test-key")
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	d.SQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`)
	d.SQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)
	it, err := d.CreateIntegration(ctx, 1, "github", "gh", "ghp_secret", "{}")
	if err != nil || it.Secret != "ghp_secret" {
		t.Fatalf("round trip: %q %v", it.Secret, err)
	}
	var raw string
	d.SQL().QueryRowContext(ctx, `SELECT secret FROM integrations WHERE id = ?`, it.ID).Scan(&raw)
	if !strings.HasPrefix(raw, sealedPrefix) || strings.Contains(raw, "ghp_secret") {
		t.Fatalf("stored in the clear: %q", raw)
	}
	// legacy plain value gets sealed on the next open-time pass
	d.SQL().ExecContext(ctx, `UPDATE teams SET stripe_key = 'sk_plain' WHERE id = 1`)
	if err := d.sealExistingSecrets(); err != nil {
		t.Fatal(err)
	}
	d.SQL().QueryRowContext(ctx, `SELECT stripe_key FROM teams WHERE id = 1`).Scan(&raw)
	if key, _, _ := d.TeamStripe(ctx, 1); !strings.HasPrefix(raw, sealedPrefix) || key != "sk_plain" {
		t.Fatalf("legacy not sealed or not readable: raw %q key %q", raw, key)
	}
	// a wrong key must not leak the blob outward
	t.Setenv("PARATRACK_SECRET_KEY", "other")
	if got, _ := d.GetIntegration(ctx, 1, it.ID); got.Secret != "" {
		t.Fatalf("wrong key returned %q", got.Secret)
	}
}
