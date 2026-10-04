package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// createActivity inserts an activity in the legacy unscoped catalog. Team
// writes must use GetOrCreateActivityForMember with an explicit caller ID.

func (d *DB) createActivity(ctx context.Context, request legacyActivityRequest) (model.Activity, error) {
	teamID, name := request.TeamID, request.Name
	if teamID != 0 {
		return model.Activity{}, model.ErrForbidden
	}
	// The name keeps the case it was typed in (it shows on invoices);
	// matching goes through name_key, lowercased in Go so Cyrillic folds
	// too.
	name = strings.TrimSpace(name)
	if name == "" {
		return model.Activity{}, fmt.Errorf("activity name cannot be empty")
	}
	now := FormatTime(d.currentTime().UTC())
	// teamID == 0 → legacy single-user path; no UNIQUE constraint to
	// collide on, just ON CONFLICT DO NOTHING.
	// Insert-if-missing then read back is enough
	// (nothing is returned when the insert was ignored).
	if _, err := d.sql.ExecContext(ctx,
		`INSERT INTO activities (name, name_key, created_at, updated_at) VALUES (?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		name, strings.ToLower(name), now, now,
	); err != nil {
		return model.Activity{}, err
	}
	return scanActivity(d.sql.QueryRowContext(ctx,
		`SELECT id, name, team_id, project_id, archived, created_at, updated_at
		 FROM activities WHERE (name_key = ? OR (name_key IS NULL AND name = ?)) AND team_id IS NULL
		 ORDER BY id LIMIT 1`, strings.ToLower(name), name))
}

// GetActivity fetches one activity from the requested workspace.
func (d *DB) GetActivity(ctx context.Context, query appmodel.ActivityLookupQuery) (model.Activity, error) {
	if query.TeamID <= 0 || query.ActivityID <= 0 {
		return model.Activity{}, ErrNotFound
	}
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, name, team_id, project_id, archived, created_at, updated_at FROM activities WHERE id = ? AND team_id = ?`, query.ActivityID, query.TeamID)
	return scanActivity(row)
}

// getActivityByName looks up by name. Inputs are lowercased so callers
// don't need to normalise; the underlying column is CITEXT
// so "Work", "work" and "WORK" all resolve to the same row. Rows written
// by an older binary during a rolling deploy may not have name_key yet.
//
// teamID restricts the lookup to one workspace; pass 0 to look across
// all teams (legacy / tests).
func (d *DB) getActivityByName(ctx context.Context, teamID int64, name string) (model.Activity, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	q := `SELECT id, name, team_id, project_id, archived, created_at, updated_at
		FROM activities WHERE (name_key = ? OR (name_key IS NULL AND name = ?))`
	args := []any{name, strings.TrimSpace(name)}
	if teamID > 0 {
		q += ` AND team_id = ?`
		args = append(args, teamID)
	}
	q += ` ORDER BY id LIMIT 1`
	row := d.sql.QueryRowContext(ctx, q, args...)
	return scanActivity(row)
}

// FindActivityByName is the case-insensitive lookup kept for callers
// that don't want to lowercase first.
func (d *DB) FindActivityByName(ctx context.Context, teamID int64, name string) (model.Activity, error) {
	return d.getActivityByName(ctx, teamID, name)
}

// getOrCreateActivity resolves or creates an activity in the legacy unscoped
// catalog. Team writes must use GetOrCreateActivityForMember with an explicit
// caller ID.

func (d *DB) getOrCreateActivity(ctx context.Context, request legacyActivityRequest) (model.Activity, error) {
	teamID, name := request.TeamID, request.Name
	if teamID != 0 {
		return model.Activity{}, model.ErrForbidden
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return model.Activity{}, fmt.Errorf("activity name cannot be empty")
	}
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, name, team_id, project_id, archived, created_at, updated_at
		 FROM activities WHERE (name_key = ? OR (name_key IS NULL AND name = ?)) AND team_id IS NULL
		 ORDER BY id LIMIT 1`, strings.ToLower(name), name)
	if a, err := scanActivity(row); err == nil {
		if _, err := d.sql.ExecContext(ctx,
			`UPDATE activities SET name_key = ? WHERE id = ? AND name_key IS NULL`, strings.ToLower(name), a.ID); err != nil {
			return model.Activity{}, err
		}
		return a, nil
	} else if !errors.Is(err, ErrNotFound) {
		return model.Activity{}, err
	}
	return d.createActivity(ctx, request)
}

// GetOrCreateActivityForMember resolves or creates an activity while holding
// the workspace lock and verifying the caller's current membership. It also
// repairs a missing name_key left by an older binary during a rolling deploy.

func (d *DB) GetOrCreateActivityForMember(ctx context.Context, request appmodel.ActivityResolveRequest) (model.Activity, error) {
	teamID, callerID, name := request.TeamID, request.CallerID, request.Name
	name = strings.TrimSpace(name)
	if teamID <= 0 || callerID <= 0 || name == "" {
		return model.Activity{}, model.ErrForbidden
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.Activity{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockCurrentTeamMember(ctx, tx, teamID, callerID); err != nil {
		return model.Activity{}, err
	}
	key := strings.ToLower(name)
	activity, err := scanActivity(tx.QueryRowContext(ctx,
		`SELECT id, name, team_id, project_id, archived, created_at, updated_at
		 FROM activities
		 WHERE team_id = ? AND (name_key = ? OR (name_key IS NULL AND name = ?))
		 ORDER BY id LIMIT 1 FOR UPDATE`, teamID, key, name))
	if err == nil {
		if _, err := tx.ExecContext(ctx,
			`UPDATE activities SET name_key = ? WHERE id = ? AND name_key IS NULL`, key, activity.ID); err != nil {
			return model.Activity{}, err
		}
		if err := tx.Commit(); err != nil {
			return model.Activity{}, err
		}
		return activity, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return model.Activity{}, err
	}
	now := FormatTime(d.currentTime().UTC())
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO activities (name, name_key, team_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		name, key, teamID, now, now); err != nil {
		return model.Activity{}, err
	}
	activity, err = scanActivity(tx.QueryRowContext(ctx,
		`SELECT id, name, team_id, project_id, archived, created_at, updated_at FROM activities WHERE name_key = ? AND team_id = ? ORDER BY id LIMIT 1`, key, teamID))
	if err != nil {
		return model.Activity{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Activity{}, err
	}
	return activity, nil
}

// ListActivities returns activities from one workspace sorted by name.
func (d *DB) ListActivities(ctx context.Context, teamID int64, includeArchived bool) ([]model.Activity, error) {
	if teamID <= 0 {
		return nil, ErrNotFound
	}
	q := `SELECT id, name, team_id, project_id, archived, created_at, updated_at FROM activities WHERE team_id = ?`
	args := []any{teamID}
	if !includeArchived {
		q += ` AND archived = 0`
	}
	q += ` ORDER BY name`
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Activity
	for rows.Next() {
		a, err := scanActivity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// row abstracts *sql.Row and *sql.Rows for shared scanning.
type row interface {
	Scan(dest ...any) error
}

func scanActivity(r row) (model.Activity, error) {
	var (
		a         model.Activity
		teamID    sql.NullInt64
		projectID sql.NullInt64
		archived  int
		created   string
		updated   string
	)
	if err := r.Scan(&a.ID, &a.Name, &teamID, &projectID, &archived, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Activity{}, ErrNotFound
		}
		return model.Activity{}, err
	}
	if teamID.Valid {
		a.TeamID = teamID.Int64
	}
	if projectID.Valid {
		a.ProjectID = projectID.Int64
	}
	a.Archived = archived != 0
	createdAt, err := ScanTime(created)
	if err != nil {
		return model.Activity{}, fmt.Errorf("parse activity creation time: %w", err)
	}
	a.CreatedAt = createdAt
	a.UpdatedAt, err = ScanTime(updated)
	if err != nil {
		return model.Activity{}, fmt.Errorf("parse activity update time: %w", err)
	}
	return a, nil
}

// ErrNotFound is returned when a single-row lookup misses.
var ErrNotFound = model.ErrNotFound

// ErrDuplicate signals a uniqueness conflict on insert.
var ErrDuplicate = model.ErrAlreadyExists
