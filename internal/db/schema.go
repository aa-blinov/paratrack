package db

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
)

// schema is the full Postgres DDL. Every statement is idempotent, so it
// runs on each start; columnMigrations and compatibilityStatements bring older
// databases up to date. Timestamps are TEXT (RFC3339Nano UTC, see
// FormatTime); case-insensitive names are CITEXT.
//
//go:embed schema.sql
var schema string

func (d *DB) applySchemaContext(ctx context.Context) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Multiple replicas may start against the same database during deploys.
	// Serialize the idempotent DDL sequence and its compatibility backfills so
	// constraint replacement and column migrations cannot race each other.
	if err := lockMigrations(ctx, tx); err != nil {
		return fmt.Errorf("lock schema migration: %w", err)
	}
	stmts := []string{
		`CREATE EXTENSION IF NOT EXISTS citext`,
		schema,
		// Older databases had these as TEXT with a case-insensitive index.
		// Avoid taking an ALTER TABLE lock when a replica starts on the current
		// schema; only legacy column types need conversion.
		`DO $$ BEGIN
		   IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'users' AND column_name = 'email' AND udt_name <> 'citext') THEN
		     ALTER TABLE users ALTER COLUMN email TYPE CITEXT;
		   END IF;
		 END $$`,
		`DO $$ BEGIN
		   IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'teams' AND column_name = 'slug' AND udt_name <> 'citext') THEN
		     ALTER TABLE teams ALTER COLUMN slug TYPE CITEXT;
		   END IF;
		 END $$`,
		// Roles grew an admin. Replace the legacy CHECK once, then leave the
		// current constraint alone on later process starts.
		`DO $$ BEGIN
		   IF NOT EXISTS (
		     SELECT 1 FROM pg_constraint
		     WHERE conrelid = 'memberships'::regclass
		       AND conname = 'memberships_role_check'
		       AND position('admin' in pg_get_constraintdef(oid)) > 0
		   ) THEN
		     ALTER TABLE memberships DROP CONSTRAINT IF EXISTS memberships_role_check;
		     ALTER TABLE memberships ADD CONSTRAINT memberships_role_check CHECK (role IN ('owner', 'admin', 'member'));
		   END IF;
		 END $$`,
	}
	// Columns go in twice: some tables are first introduced by
	// compatibilityStatements, and their indexes need the columns.
	cols := func() {
		for _, m := range columnMigrations {
			stmts = append(stmts, "ALTER TABLE IF EXISTS "+m.table+" ADD COLUMN IF NOT EXISTS "+m.column+" "+m.decl)
		}
	}
	cols()
	stmts = append(stmts, compatibilityStatements...)
	cols()
	stmts = append(stmts, `INSERT INTO invoice_stripe_sessions (stripe_session_id, team_id, invoice_id, created_at)
		SELECT stripe_session_id, team_id, id, created_at FROM invoices
		WHERE COALESCE(stripe_session_id, '') <> ''
		ON CONFLICT (stripe_session_id) DO NOTHING`)
	// Closed sessions carry their tracked total (older ones left it 0 and
	// had it recomputed from the text timestamps on every read). Same
	// number DurationSeconds gives: whole seconds of the span.
	stmts = append(stmts, `UPDATE sessions SET accumulated_seconds =
		GREATEST(0, FLOOR(EXTRACT(EPOCH FROM (end_at::timestamp - start_at::timestamp))))::bigint
		WHERE end_at IS NOT NULL AND accumulated_seconds = 0 AND end_at > start_at`)
	// A session's invoice is a real reference: deleting the invoice frees
	// the session, so "not billed" is just invoice_id IS NULL.
	stmts = append(stmts,
		`UPDATE sessions SET invoice_id = NULL WHERE invoice_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM invoices i WHERE i.id = sessions.invoice_id)`,
		`DO $$ BEGIN
		   IF NOT EXISTS (
		     SELECT 1 FROM pg_constraint
		     WHERE conrelid = 'sessions'::regclass
		       AND conname = 'sessions_invoice_id_fkey'
		   ) THEN
		     ALTER TABLE sessions ADD CONSTRAINT sessions_invoice_id_fkey FOREIGN KEY (invoice_id) REFERENCES invoices(id) ON DELETE SET NULL;
		   END IF;
		 END $$`,
	)
	for _, s := range stmts {
		// Use the raw transaction because the schema is PostgreSQL DDL and may
		// contain multiple statements, not query placeholders to rebind.
		if err := tx.execRawContext(ctx, s); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// caseRow is an (id, name) pair.
type caseRow struct {
	id   int64
	name string
}

func (d *DB) migrateActivityKeys(ctx context.Context) error {
	// Old binaries in a rolling deployment can still insert activities without
	// name_key. Keep this backfill repeatable, but serialize replicas with the
	// schema lock and avoid replacing a key changed after the read.
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin activity key backfill: %w", err)
	}
	defer tx.Rollback()
	if err := lockMigrations(ctx, tx); err != nil {
		return fmt.Errorf("lock activity key backfill: %w", err)
	}
	var lastID int64
	for {
		rows, err := tx.QueryContext(ctx, `SELECT id, name FROM activities
			WHERE id > ? AND name_key IS NULL ORDER BY id LIMIT ?`, lastID, migrationBatchSize)
		if err != nil {
			return fmt.Errorf("read activities for key backfill: %w", err)
		}
		batch := make([]caseRow, 0, migrationBatchSize)
		for rows.Next() {
			var row caseRow
			if err := rows.Scan(&row.id, &row.name); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan activity key backfill row: %w", err)
			}
			batch = append(batch, row)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("iterate activities for key backfill: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close activity key backfill rows: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		lastID = batch[len(batch)-1].id
		for _, row := range batch {
			if _, err := tx.ExecContext(ctx,
				`UPDATE activities SET name_key = ? WHERE id = ? AND name_key IS NULL AND name = ?`, strings.ToLower(row.name), row.id, row.name); err != nil {
				return fmt.Errorf("write activity key for id %d: %w", row.id, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit activity key backfill: %w", err)
	}
	return nil
}
