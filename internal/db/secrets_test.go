package db

import (
	"context"
	"database/sql"
	"time"
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

// Invoices from before invoice_id get their sessions stamped once, by the
// old billing rule; newer sessions in that period stay billable.
func TestStampLegacyInvoices(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	d.SQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`)
	d.SQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)
	p, _ := d.CreateProject(ctx, 1, "Acme", "", "#7c3aed")
	rate := 1000
	d.SetProjectRate(ctx, 1, p.ID, &rate, nil)
	a, _ := d.CreateActivity(ctx, 1, "work")
	d.AssignActivityProject(ctx, 1, a.ID, p.ID)
	day := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	old, _ := d.CreateClosedSession(ctx, 1, a.ID, day, day.Add(time.Hour), "")
	lines, _ := d.BuildInvoiceLines(ctx, 1, day.Add(-time.Hour), day.AddDate(0, 0, 1), 0)
	inv, err := d.CreateInvoice(ctx, 1, "INV-OLD", "Acme", day.Add(-time.Hour), day.AddDate(0, 0, 1), "", lines)
	if err != nil {
		t.Fatal(err)
	}
	// Pretend both predate invoice_id.
	d.SQL().ExecContext(ctx, `UPDATE sessions SET invoice_id = NULL, created_at = '2026-08-03T10:00:00Z'`)
	d.SQL().ExecContext(ctx, `UPDATE invoices SET created_at = '2026-08-04T00:00:00Z' WHERE id = ?`, inv.ID)
	late, _ := d.CreateClosedSession(ctx, 1, a.ID, day.Add(2*time.Hour), day.Add(3*time.Hour), "") // added after the invoice
	if err := d.stampLegacyInvoices(ctx); err != nil {
		t.Fatal(err)
	}
	var oldInv, lateInv sql.NullInt64
	d.SQL().QueryRowContext(ctx, `SELECT invoice_id FROM sessions WHERE id = ?`, old.ID).Scan(&oldInv)
	d.SQL().QueryRowContext(ctx, `SELECT invoice_id FROM sessions WHERE id = ?`, late.ID).Scan(&lateInv)
	if oldInv.Int64 != inv.ID || lateInv.Valid {
		t.Fatalf("old session → %v (want %d), late session → %v (want none)", oldInv, inv.ID, lateInv)
	}
}
