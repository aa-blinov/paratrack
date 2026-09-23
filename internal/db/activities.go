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

// CreateActivity inserts a new activity. Returns ErrDuplicate if the name
// already exists (the column has a UNIQUE constraint).
func (d *DB) CreateActivity(ctx context.Context, name string) (model.Activity, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return model.Activity{}, fmt.Errorf("activity name cannot be empty")
	}
	now := FormatTime(time.Now().UTC())
	res, err := d.sql.ExecContext(ctx,
		`INSERT OR IGNORE INTO activities (name, created_at, updated_at) VALUES (?, ?, ?)`,
		name, now, now,
	)
	if err != nil {
		return model.Activity{}, err
	}
	id, err := res.LastInsertId()
	if err != nil || id == 0 {
		// Race: someone else inserted it; fetch existing.
		act, err := d.GetActivityByName(ctx, name)
		if err != nil {
			return model.Activity{}, err
		}
		return act, nil
	}
	return d.GetActivity(ctx, id)
}

// GetActivity fetches one activity by id. Returns ErrNotFound when missing.
func (d *DB) GetActivity(ctx context.Context, id int64) (model.Activity, error) {
	row := d.sql.QueryRowContext(ctx, `SELECT id, name, archived, created_at, updated_at FROM activities WHERE id = ?`, id)
	return scanActivity(row)
}

// GetActivityByName looks up by name. Inputs are lowercased so callers
// don't need to normalise; the underlying column is COLLATE NOCASE
// so "Work", "work" and "WORK" all resolve to the same row.
func (d *DB) GetActivityByName(ctx context.Context, name string) (model.Activity, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	row := d.sql.QueryRowContext(ctx, `SELECT id, name, archived, created_at, updated_at FROM activities WHERE name = ?`, name)
	return scanActivity(row)
}

// FindActivityByName is the case-insensitive lookup kept for callers
// that don't want to lowercase first (legacy alias of GetActivityByName
// now that the column itself is COLLATE NOCASE).
func (d *DB) FindActivityByName(ctx context.Context, name string) (model.Activity, error) {
	return d.GetActivityByName(ctx, name)
}

// GetOrCreateActivity returns the existing activity (matched case-insensitive)
// or inserts a new one. Both CLI quick-start and the web form rely on this.
func (d *DB) GetOrCreateActivity(ctx context.Context, name string) (model.Activity, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return model.Activity{}, fmt.Errorf("activity name cannot be empty")
	}
	row := d.sql.QueryRowContext(ctx, `SELECT id, name, archived, created_at, updated_at FROM activities WHERE name = ?`, name)
	if a, err := scanActivity(row); err == nil {
		return a, nil
	} else if !errors.Is(err, ErrNotFound) {
		return model.Activity{}, err
	}
	return d.CreateActivity(ctx, name)
}

// ListActivities returns non-archived activities sorted by name.
func (d *DB) ListActivities(ctx context.Context, includeArchived bool) ([]model.Activity, error) {
	q := `SELECT id, name, archived, created_at, updated_at FROM activities`
	if !includeArchived {
		q += ` WHERE archived = 0`
	}
	q += ` ORDER BY name COLLATE NOCASE`
	rows, err := d.sql.QueryContext(ctx, q)
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
		a        model.Activity
		archived int
		created  string
		updated  string
	)
	if err := r.Scan(&a.ID, &a.Name, &archived, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Activity{}, ErrNotFound
		}
		return model.Activity{}, err
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
