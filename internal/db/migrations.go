package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

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

// migrateProjectsToClients turns the free-text recipient columns that used to
// live on a project into real client cards.
//
// Before this, "who is billed" was three columns on the project, so two
// projects for the same customer were two unrelated records and every
// invoice retyped the address. Each distinct recipient name per workspace
// becomes one card, and every project carrying that name is linked to it.
//
// Deliberate details:
//   - Names are matched case-insensitively, because clients.name is CITEXT:
//     "ООО Кошелька" and "ооо кошелька" are the same customer and must not
//     become two cards that each claim half of the history.
//   - The first spelling and the first non-empty email/address win. The
//     snapshot columns on the project are left untouched, so an invoice that
//     was already sent still shows what it showed.
//   - Runs once, under the shared migration lock, and inside the startup
//     sequence — after the schema step that adds the new columns.
func (d *DB) migrateProjectsToClients(ctx context.Context) error {
	_, err := d.runDataMigration(ctx, "20261012_projects_to_clients", func(tx *Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT team_id, client_name, client_email, client_details FROM projects
			  WHERE TRIM(client_name) <> '' ORDER BY id`)
		if err != nil {
			return fmt.Errorf("read projects for client backfill: %w", err)
		}
		type seed struct {
			teamID  int64
			name    string
			email   string
			details string
		}
		var order []string
		seeds := map[string]*seed{}
		for rows.Next() {
			var teamID int64
			var name, email, details string
			if err := rows.Scan(&teamID, &name, &email, &details); err != nil {
				rows.Close()
				return fmt.Errorf("scan project for client backfill: %w", err)
			}
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			key := fmt.Sprintf("%d\x00%s", teamID, strings.ToLower(name))
			existing, seen := seeds[key]
			if !seen {
				seeds[key] = &seed{teamID: teamID, name: name, email: strings.TrimSpace(email), details: strings.TrimSpace(details)}
				order = append(order, key)
				continue
			}
			// Same customer written differently: keep what we already have
			// and only fill the blanks.
			if existing.email == "" {
				existing.email = strings.TrimSpace(email)
			}
			if existing.details == "" {
				existing.details = strings.TrimSpace(details)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("walk projects for client backfill: %w", err)
		}
		rows.Close()

		now := FormatTime(d.currentTime().UTC())
		for _, key := range order {
			s := seeds[key]
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO clients (team_id, name, email, details, currency, notes, created_at, updated_at)
				 VALUES (?, ?, ?, ?, '', '', ?, ?)
				 ON CONFLICT (team_id, name) DO NOTHING`,
				s.teamID, s.name, s.email, s.details, now, now); err != nil {
				return fmt.Errorf("create client %q during backfill: %w", s.name, err)
			}
			var clientID int64
			if err := tx.QueryRowContext(ctx,
				`SELECT id FROM clients WHERE team_id = ? AND name = ?`, s.teamID, s.name).Scan(&clientID); err != nil {
				return fmt.Errorf("read back client %q after backfill: %w", s.name, err)
			}
			if _, err := tx.ExecContext(ctx,
				`UPDATE projects SET client_id = ? WHERE team_id = ? AND client_id IS NULL AND LOWER(TRIM(client_name)) = ?`,
				clientID, s.teamID, strings.ToLower(s.name)); err != nil {
				return fmt.Errorf("link projects to client %q: %w", s.name, err)
			}
		}
		return nil
	})
	return err
}
