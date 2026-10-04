package db

import (
	"context"
	"fmt"
)

const advisoryMigrationLockSQL = `SELECT pg_advisory_xact_lock(7341601, 1)`
const migrationBatchSize = 128

func lockMigrations(ctx context.Context, tx *Tx) error {
	return tx.execRawContext(ctx, advisoryMigrationLockSQL)
}

// runDataMigration applies one compatibility backfill once across all
// application instances. Schema changes and data migrations share the same
// transaction-scoped lock so startup steps stay ordered during deploys.
func (d *DB) runDataMigration(ctx context.Context, name string, apply func(*Tx) error) (bool, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin data migration %q: %w", name, err)
	}
	defer tx.Rollback()
	if err := lockMigrations(ctx, tx); err != nil {
		return false, fmt.Errorf("lock data migration %q: %w", name, err)
	}
	if err := tx.execRawContext(ctx, `CREATE TABLE IF NOT EXISTS paratrack_migrations (
		name TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return false, fmt.Errorf("create migration history: %w", err)
	}
	var applied bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM paratrack_migrations WHERE name = ?)`, name).Scan(&applied); err != nil {
		return false, fmt.Errorf("check migration history for %q: %w", name, err)
	}
	if applied {
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("commit migration check for %q: %w", name, err)
		}
		return false, nil
	}
	if err := apply(tx); err != nil {
		return false, fmt.Errorf("apply data migration %q: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO paratrack_migrations(name) VALUES (?)`, name); err != nil {
		return false, fmt.Errorf("record data migration %q: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit data migration %q: %w", name, err)
	}
	return true, nil
}
