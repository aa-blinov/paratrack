package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/aa-blinov/paratrack/internal/model"
)

// ErrTagNotFound is returned when a tag is missing.
var ErrTagNotFound = errors.New("tag not found")

// CreateTag inserts a new tag or returns the existing one if the name
// is already taken. Tags are unique by name (case-insensitive at the
// storage layer — SQLite's default TEXT comparison is binary).
func (d *DB) CreateTag(ctx context.Context, name string) (model.Tag, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return model.Tag{}, fmt.Errorf("tag name cannot be empty")
	}
	// INSERT OR IGNORE + lookup keeps this idempotent.
	if _, err := d.sql.ExecContext(ctx,
		`INSERT OR IGNORE INTO tags (name) VALUES (?)`, name,
	); err != nil {
		return model.Tag{}, err
	}
	return d.GetTagByName(ctx, name)
}

// GetTagByName returns the tag with the given name. Inputs are lowercased
// so callers don't need to normalise; the underlying column is
// COLLATE NOCASE so mixed-case input still resolves correctly.
func (d *DB) GetTagByName(ctx context.Context, name string) (model.Tag, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, name, created_at FROM tags WHERE name = ?`, name)
	return scanTag(row)
}

// ListTags returns every tag sorted alphabetically.
func (d *DB) ListTags(ctx context.Context) ([]model.Tag, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, name, created_at FROM tags ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Tag
	for rows.Next() {
		t, err := scanTag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteTag removes the tag and (via FK cascade) drops all session_tags
// rows pointing to it.
func (d *DB) DeleteTag(ctx context.Context, id int64) error {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM tags WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTagNotFound
	}
	return nil
}

// AttachTag links an existing tag to a session. Both id-based and
// name-based lookups go through here so the callers don't need to
// preload the tag themselves.
func (d *DB) AttachTag(ctx context.Context, sessionID int64, tagName string) error {
	tag, err := d.GetTagByName(ctx, tagName)
	if err != nil {
		// Auto-create on first attach — matches the "just type the tag"
		// UX of the web UI.
		if errors.Is(err, ErrTagNotFound) {
			var cerr error
			tag, cerr = d.CreateTag(ctx, tagName)
			if cerr != nil {
				return fmt.Errorf("create tag: %w", cerr)
			}
		} else {
			return err
		}
	}
	_, err = d.sql.ExecContext(ctx,
		`INSERT OR IGNORE INTO session_tags (session_id, tag_id) VALUES (?, ?)`,
		sessionID, tag.ID)
	return err
}

// DetachTag removes the link between a session and a tag. Does not
// delete the tag itself.
func (d *DB) DetachTag(ctx context.Context, sessionID int64, tagName string) error {
	tag, err := d.GetTagByName(ctx, tagName)
	if err != nil {
		return err
	}
	_, err = d.sql.ExecContext(ctx,
		`DELETE FROM session_tags WHERE session_id = ? AND tag_id = ?`,
		sessionID, tag.ID)
	return err
}

// SetTagsForSession replaces the tag set for a session with the given
// names. Tags not yet in the catalogue are auto-created. Used by the
// stats inline-edit form where the user picks chips.
func (d *DB) SetTagsForSession(ctx context.Context, sessionID int64, tagNames []string) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM session_tags WHERE session_id = ?`, sessionID,
	); err != nil {
		return err
	}
	for _, raw := range tagNames {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		var tagID int64
		if err := tx.QueryRowContext(ctx,
			`SELECT id FROM tags WHERE name = ?`, name,
		).Scan(&tagID); err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			// Create.
			res, err := tx.ExecContext(ctx, `INSERT INTO tags (name) VALUES (?)`, name)
			if err != nil {
				return err
			}
			tagID, err = res.LastInsertId()
			if err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO session_tags (session_id, tag_id) VALUES (?, ?)`,
			sessionID, tagID,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListTagsForSession returns every tag attached to the given session,
// ordered alphabetically.
func (d *DB) ListTagsForSession(ctx context.Context, sessionID int64) ([]model.Tag, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT t.id, t.name, t.created_at
		FROM tags t
		JOIN session_tags st ON st.tag_id = t.id
		WHERE st.session_id = ?
		ORDER BY t.name`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Tag
	for rows.Next() {
		t, err := scanTag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TagsForSessions returns tags grouped by session id. Sessions with no
// tags simply don't appear in the result map. Designed for batch
// hydration when rendering a long list of sessions — one SQL round-trip
// instead of N.
func (d *DB) TagsForSessions(ctx context.Context, sessionIDs []int64) (map[int64][]model.Tag, error) {
	out := make(map[int64][]model.Tag, len(sessionIDs))
	if len(sessionIDs) == 0 {
		return out, nil
	}
	placeholders := strings.Repeat("?,", len(sessionIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(sessionIDs))
	for i, id := range sessionIDs {
		args[i] = id
	}
	q := `SELECT st.session_id, t.id, t.name, t.created_at
	      FROM session_tags st
	      JOIN tags t ON t.id = st.tag_id
	      WHERE st.session_id IN (` + placeholders + `)
	      ORDER BY st.session_id, t.name`
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			sid       int64
			t         model.Tag
			createdAt string
		)
		if err := rows.Scan(&sid, &t.ID, &t.Name, &createdAt); err != nil {
			return nil, err
		}
		if ts, err := ScanTime(createdAt); err == nil {
			t.CreatedAt = ts
		}
		out[sid] = append(out[sid], t)
	}
	return out, rows.Err()
}

// ListSessionsByTag returns every closed session that carries the
// given tag, newest first. Used by the tag-filter on the stats page.
func (d *DB) ListSessionsByTag(ctx context.Context, tagName string) ([]model.ActiveSession, error) {
	tag, err := d.GetTagByName(ctx, tagName)
	if err != nil {
		return nil, err
	}
	q := sessionSelect + `
		JOIN session_tags st ON st.session_id = s.id
		WHERE s.end_at IS NOT NULL
		  AND st.tag_id = ?
		ORDER BY s.start_at DESC`
	rows, err := d.sql.QueryContext(ctx, q, tag.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanActiveSessions(rows)
}

// ListAllTagsWithCounts returns every tag with the number of sessions
// (active + closed) that carry it. Used by the /tags page.
type TagWithCount struct {
	model.Tag
	SessionCount int
}

func (d *DB) ListAllTagsWithCounts(ctx context.Context) ([]TagWithCount, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT t.id, t.name, t.created_at, COUNT(st.session_id)
		FROM tags t
		LEFT JOIN session_tags st ON st.tag_id = t.id
		GROUP BY t.id
		ORDER BY COUNT(st.session_id) DESC, t.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TagWithCount
	for rows.Next() {
		var (
			tc        TagWithCount
			createdAt string
		)
		if err := rows.Scan(&tc.ID, &tc.Name, &createdAt, &tc.SessionCount); err != nil {
			return nil, err
		}
		if ts, err := ScanTime(createdAt); err == nil {
			tc.CreatedAt = ts
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}

func scanTag(r row) (model.Tag, error) {
	var t model.Tag
	var createdAt string
	if err := r.Scan(&t.ID, &t.Name, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Tag{}, ErrTagNotFound
		}
		return model.Tag{}, err
	}
	if ts, err := ScanTime(createdAt); err == nil {
		t.CreatedAt = ts
	}
	return t, nil
}