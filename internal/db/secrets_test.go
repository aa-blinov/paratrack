package db

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestSecretsSealedAtRest(t *testing.T) {
	t.Setenv("PARATRACK_SECRET_KEY", "test-key")
	d, err := OpenTest(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	d.TestSQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`)
	d.TestSQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)
	d.TestSQL().ExecContext(ctx, `INSERT INTO memberships (team_id, user_id, role) VALUES (1,1,'owner')`)
	it, err := d.CreateIntegration(ctx, appmodel.IntegrationCreateRequest{
		TeamID: 1, CallerID: 1, Provider: "github", Name: "gh", Secret: "ghp_secret", Config: appmodel.IntegrationConfig{},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := appmodel.IntegrationSyncStartRequest{TeamID: 1, IntegrationID: it.ID, CallerID: 1}
	credentials, err := d.BeginIntegrationSync(ctx, request)
	if err != nil || credentials.Secret != "ghp_secret" {
		t.Fatalf("round trip: %q %v", credentials.Secret, err)
	}
	var raw string
	d.TestSQL().QueryRowContext(ctx, `SELECT secret FROM integrations WHERE id = ?`, it.ID).Scan(&raw)
	if !strings.HasPrefix(raw, sealedPrefix) || strings.Contains(raw, "ghp_secret") {
		t.Fatalf("stored in the clear: %q", raw)
	}
	// legacy plain value gets sealed on the next open-time pass
	d.TestSQL().ExecContext(ctx, `UPDATE teams SET stripe_key = 'sk_plain' WHERE id = 1`)
	if err := d.sealExistingSecretsContext(ctx); err != nil {
		t.Fatal(err)
	}
	d.TestSQL().QueryRowContext(ctx, `SELECT stripe_key FROM teams WHERE id = 1`).Scan(&raw)
	if key, _, _ := d.TeamStripe(ctx, 1); !strings.HasPrefix(raw, sealedPrefix) || key != "sk_plain" {
		t.Fatalf("legacy not sealed or not readable: raw %q key %q", raw, key)
	}
	// The open DB keeps its configured key even if the process environment
	// changes. A new DB instance with the wrong key rejects existing ciphertext.
	t.Setenv("PARATRACK_SECRET_KEY", "other")
	got, err := d.BeginIntegrationSync(ctx, request)
	if err != nil || got.Secret != "ghp_secret" {
		t.Fatalf("changing env altered the open DB's key: %q", got.Secret)
	}
	d.secrets = newSecretCodec("other")
	if err := d.sealExistingSecretsContext(ctx); err == nil {
		t.Fatal("wrong key should fail secret validation")
	}
	if unreadable, err := d.BeginIntegrationSync(ctx, request); err == nil || unreadable.Secret != "" {
		t.Fatalf("wrong key returned %q with error %v", unreadable.Secret, err)
	}
	var generation int64
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT sync_generation FROM integrations WHERE id = ?`, it.ID).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	if generation != got.Generation {
		t.Fatalf("failed credential decryption advanced generation to %d, want %d", generation, got.Generation)
	}
}

func TestSecretMigrationValidatesCiphertextsBeforeUpdatingLegacyValues(t *testing.T) {
	t.Setenv("PARATRACK_SECRET_KEY", "correct-key")
	d, err := OpenTest(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	userID, teamID, err := d.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: "migration@example.com", PasswordHash: "hash", Name: "Migration", TeamName: "Migration workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO integrations (team_id, provider, name, secret, config, created_at) VALUES (?, 'github', 'legacy', 'plain-legacy-secret', '{}', ?)`,
		teamID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	ciphertext, err := d.sealSecret("stripe-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE teams SET stripe_key = ? WHERE owner_id = ?`, ciphertext, userID); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PARATRACK_SECRET_KEY", "wrong-key")
	d.secrets = newSecretCodec("wrong-key")
	if err := d.sealExistingSecretsContext(ctx); err == nil {
		t.Fatal("migration with the wrong key should fail")
	}
	var stored string
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT secret FROM integrations WHERE team_id = ? AND name = 'legacy'`, teamID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "plain-legacy-secret" {
		t.Fatalf("legacy secret changed before ciphertext validation: %q", stored)
	}
}

func TestSecretMigrationSealsMultipleBatches(t *testing.T) {
	t.Setenv("PARATRACK_SECRET_KEY", "batch-migration-key")
	d, err := OpenTest(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, teamID, err := d.CreateAccount(ctx, appmodel.AccountCreateRequest{
		Email: "batch@example.com", PasswordHash: "hash", Name: "Batch", TeamName: "Batch workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	const count = migrationBatchSize*2 + 1
	if _, err := d.TestSQL().ExecContext(ctx, `INSERT INTO integrations (team_id, provider, name, secret, config, created_at)
		SELECT ?, 'github', 'legacy-' || n, 'plain-secret-' || n, '{}', ?
		FROM generate_series(1, ?) AS series(n)`, teamID, FormatTime(time.Now().UTC()), count); err != nil {
		t.Fatal(err)
	}
	if err := d.sealExistingSecretsContext(ctx); err != nil {
		t.Fatal(err)
	}
	var plaintext int
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT count(*) FROM integrations WHERE team_id = ? AND secret NOT LIKE ?`, teamID, sealedPrefix+"%").Scan(&plaintext); err != nil {
		t.Fatal(err)
	}
	if plaintext != 0 {
		t.Fatalf("plaintext secrets after migration = %d, want 0", plaintext)
	}
}

// Invoices from before invoice_id get their sessions stamped once, by the
// old billing rule; newer sessions in that period stay billable.
func TestStampLegacyInvoices(t *testing.T) {
	d, err := OpenTest(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// OpenTest runs startup migrations. Re-arm this one in the isolated test
	// schema so fixtures can exercise its first application explicitly.
	if _, err := d.TestSQL().ExecContext(ctx, `DELETE FROM paratrack_migrations WHERE name = '20260927_stamp_legacy_invoice_sessions'`); err != nil {
		t.Fatal(err)
	}
	d.TestSQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`)
	d.TestSQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)
	d.TestSQL().ExecContext(ctx, `INSERT INTO memberships (team_id, user_id, role) VALUES (1,1,'owner')`)
	ctx = requestctx.WithActor(ctx, 1)
	p, _ := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: 1, CallerID: 1, Name: "Acme", Slug: "", Color: "#7c3aed"})
	rate := 1000
	d.SetProjectRate(ctx, appmodel.ProjectRateRequest{TeamID: 1, ProjectID: p.ID, CallerID: 1, RateCents: &rate})
	a, _ := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: 1, CallerID: 1, Name: "work"})
	d.AssignActivityProject(ctx, appmodel.AssignActivityProjectRequest{TeamID: 1, ActivityID: a.ID, ProjectID: p.ID, CallerID: 1})
	day := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	old, _ := d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: 1, ActivityID: a.ID, Start: day, End: day.Add(time.Hour)})
	lines, _ := d.BuildInvoiceLines(ctx, 1, day.Add(-time.Hour), day.AddDate(0, 0, 1), 0)
	inv, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: 1, CallerID: 1, Number: "INV-OLD", Client: "Acme", Start: day.Add(-time.Hour), End: day.AddDate(0, 0, 1), Lines: lines})
	if err != nil {
		t.Fatal(err)
	}
	// Pretend both predate invoice_id.
	d.TestSQL().ExecContext(ctx, `UPDATE sessions SET invoice_id = NULL, created_at = '2026-08-03T10:00:00Z'`)
	d.TestSQL().ExecContext(ctx, `UPDATE invoices SET created_at = '2026-08-04T00:00:00Z' WHERE id = ?`, inv.ID)
	late, _ := d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: 1, ActivityID: a.ID, Start: day.Add(2 * time.Hour), End: day.Add(3 * time.Hour)}) // added after the invoice
	if err := d.stampLegacyInvoices(ctx); err != nil {
		t.Fatal(err)
	}
	var oldInv, lateInv sql.NullInt64
	d.TestSQL().QueryRowContext(ctx, `SELECT invoice_id FROM sessions WHERE id = ?`, old.ID).Scan(&oldInv)
	d.TestSQL().QueryRowContext(ctx, `SELECT invoice_id FROM sessions WHERE id = ?`, late.ID).Scan(&lateInv)
	if oldInv.Int64 != inv.ID || lateInv.Valid {
		t.Fatalf("old session → %v (want %d), late session → %v (want none)", oldInv, inv.ID, lateInv)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE sessions SET created_at = '2026-08-03T10:00:00Z' WHERE id = ?`, late.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.stampLegacyInvoices(ctx); err != nil {
		t.Fatal(err)
	}
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT invoice_id FROM sessions WHERE id = ?`, late.ID).Scan(&lateInv); err != nil {
		t.Fatal(err)
	}
	if lateInv.Valid {
		t.Fatalf("completed legacy invoice migration ran again and stamped session %d", late.ID)
	}
	var migrationCount int
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT count(*) FROM paratrack_migrations WHERE name = '20260927_stamp_legacy_invoice_sessions'`).Scan(&migrationCount); err != nil {
		t.Fatal(err)
	}
	if migrationCount != 1 {
		t.Fatalf("legacy invoice migration recorded %d times, want 1", migrationCount)
	}
}
