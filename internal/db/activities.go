package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

// CreateActivity inserts a new activity scoped to teamID. Pass 0 for
// "no team" — useful in unit tests that pre-date the team layer.
// Returns ErrDuplicate if the name already exists in the same team.
func (d *DB) CreateActivity(ctx context.Context, teamID int64, name string) (model.Activity, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return model.Activity{}, fmt.Errorf("activity name cannot be empty")
	}
	now := FormatTime(time.Now().UTC())
	if teamID > 0 {
		var id int64
		err := d.sql.QueryRowContext(ctx,
			`INSERT INTO activities (name, team_id, created_at, updated_at) VALUES (?, ?, ?, ?) RETURNING id`,
			name, teamID, now, now,
		).Scan(&id)
		if err != nil {
			if isUniqueViolation(err) {
				return d.GetActivityByName(ctx, teamID, name)
			}
			return model.Activity{}, err
		}
		if err != nil || id == 0 {
			return d.GetActivityByName(ctx, teamID, name)
		}
		return d.GetActivity(ctx, id)
	}
	// teamID == 0 → legacy single-user path; no UNIQUE constraint to
	// collide on, just INSERT OR IGNORE.
	// Insert-if-missing then read back works the same on both backends
	// (a RETURNING row is absent when the insert was ignored).
	if _, err := d.sql.ExecContext(ctx,
		`INSERT OR IGNORE INTO activities (name, created_at, updated_at) VALUES (?, ?, ?)`,
		name, now, now,
	); err != nil {
		return model.Activity{}, err
	}
	return d.GetActivityByName(ctx, 0, name)
}

// GetActivity fetches one activity by id. teamID restricts the lookup
// to a workspace; pass 0 to skip the check (legacy / tests).
func (d *DB) GetActivity(ctx context.Context, id int64) (model.Activity, error) {
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, name, team_id, project_id, archived, created_at, updated_at FROM activities WHERE id = ?`, id)
	return scanActivity(row)
}

// GetActivityByName looks up by name. Inputs are lowercased so callers
// don't need to normalise; the underlying column is COLLATE NOCASE
// so "Work", "work" and "WORK" all resolve to the same row.
//
// teamID restricts the lookup to one workspace; pass 0 to look across
// all teams (legacy / tests).
func (d *DB) GetActivityByName(ctx context.Context, teamID int64, name string) (model.Activity, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	q := `SELECT id, name, team_id, project_id, archived, created_at, updated_at FROM activities WHERE name = ?`
	args := []any{name}
	if teamID > 0 {
		q += ` AND team_id = ?`
		args = append(args, teamID)
	}
	row := d.sql.QueryRowContext(ctx, q, args...)
	return scanActivity(row)
}

// FindActivityByName is the case-insensitive lookup kept for callers
// that don't want to lowercase first.
func (d *DB) FindActivityByName(ctx context.Context, teamID int64, name string) (model.Activity, error) {
	return d.GetActivityByName(ctx, teamID, name)
}

// GetOrCreateActivity returns the existing activity or inserts a new
// one. Both CLI quick-start and the web form rely on this.
func (d *DB) GetOrCreateActivity(ctx context.Context, teamID int64, name string) (model.Activity, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return model.Activity{}, fmt.Errorf("activity name cannot be empty")
	}
	q := `SELECT id, name, team_id, project_id, archived, created_at, updated_at FROM activities WHERE name = ?`
	args := []any{name}
	if teamID > 0 {
		q += ` AND team_id = ?`
		args = append(args, teamID)
	}
	row := d.sql.QueryRowContext(ctx, q, args...)
	if a, err := scanActivity(row); err == nil {
		return a, nil
	} else if !errors.Is(err, ErrNotFound) {
		return model.Activity{}, err
	}
	return d.CreateActivity(ctx, teamID, name)
}

// ListActivities returns non-archived activities sorted by name,
// scoped to teamID (0 means "all teams").
func (d *DB) ListActivities(ctx context.Context, teamID int64, includeArchived bool) ([]model.Activity, error) {
	q := `SELECT id, name, team_id, project_id, archived, created_at, updated_at FROM activities`
	args := []any{}
	if teamID > 0 {
		q += ` WHERE team_id = ?`
		args = append(args, teamID)
	}
	if !includeArchived {
		if teamID > 0 {
			q += ` AND`
		} else {
			q += ` WHERE`
		}
		q += ` archived = 0`
	}
	q += ` ORDER BY name COLLATE NOCASE`
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
	if t, err := ScanTime(created); err == nil {
		a.CreatedAt = t
	}
	if t, err := ScanTime(updated); err == nil {
		a.UpdatedAt = t
	}
	return a, nil
}

// ErrNotFound is returned when a single-row lookup misses.
var ErrNotFound = errors.New("not found")

// ErrDuplicate signals a uniqueness conflict on insert.
var ErrDuplicate = fmt.Errorf("duplicate")