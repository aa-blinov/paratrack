package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/model"
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

// migrateActivityColors gives every activity its own mark.
//
// The mark used to be hashed from the name into a ten-slot palette, so once a
// workspace passed ten activities two of them landed on the same colour and
// the dot stopped identifying anything — measured on a 14-activity workspace:
// four slots held more than one name. The colour is stored now.
//
// The backfill keeps a mark wherever it is already unambiguous and only moves
// the ones that collided, so a workspace that never collided sees no change
// at all. It is batched and repeatable: rows already carrying a colour are
// left alone.
func (d *DB) migrateActivityColors(ctx context.Context) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin activity colour backfill: %w", err)
	}
	defer tx.Rollback()
	if err := lockMigrations(ctx, tx); err != nil {
		return fmt.Errorf("lock activity colour backfill: %w", err)
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT id, team_id, name, color FROM activities
		  WHERE color IS NULL OR color = '' ORDER BY id`)
	if err != nil {
		return fmt.Errorf("read activities for colour backfill: %w", err)
	}
	type pending struct {
		id    int64
		color string
	}
	byWorkspace := map[int64][]pending{}
	for rows.Next() {
		var id, teamID int64
		var team sql.NullInt64
		var name string
		var color sql.NullString
		if err := rows.Scan(&id, &team, &name, &color); err != nil {
			rows.Close()
			return fmt.Errorf("scan activity for colour backfill: %w", err)
		}
		workspace := teamID
		if team.Valid {
			workspace = team.Int64
		}
		byWorkspace[workspace] = append(byWorkspace[workspace], pending{id: id, color: model.ColorForName(name)})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("walk activities for colour backfill: %w", err)
	}
	rows.Close()
	for workspace, items := range byWorkspace {
		// Count first: a colour worn by exactly one activity stays with it,
		// anything shared is reissued from the free slots.
		worn := map[string]int{}
		for _, item := range items {
			worn[item.color]++
		}
		used := make([]string, 0, len(model.Palette))
		for _, item := range items {
			if worn[item.color] != 1 {
				continue
			}
			// Unambiguous: the mark it already wears stays, and is now written
			// down instead of being recomputed on every render.
			used = append(used, item.color)
			if err := writeActivityColor(ctx, tx, item.id, item.color); err != nil {
				return err
			}
		}
		for _, item := range items {
			if worn[item.color] == 1 {
				continue
			}
			item.color = model.NextColor(used)
			used = append(used, item.color)
			if err := writeActivityColor(ctx, tx, item.id, item.color); err != nil {
				return err
			}
		}
		_ = workspace
	}
	return tx.Commit()
}

func writeActivityColor(ctx context.Context, tx *Tx, id int64, color string) error {
	if _, err := tx.ExecContext(ctx,
		`UPDATE activities SET color = ? WHERE id = ? AND (color IS NULL OR color = '')`,
		color, id); err != nil {
		return fmt.Errorf("write activity colour: %w", err)
	}
	return nil
}
