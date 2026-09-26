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

// CreateTag inserts a new tag in the given team or returns the existing
// one if the name is already taken in that team. teamID == 0 falls
// back to the legacy "no team" path.
func (d *DB) CreateTag(ctx context.Context, teamID int64, name string) (model.Tag, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return model.Tag{}, fmt.Errorf("tag name cannot be empty")
	}
	if teamID > 0 {
		if _, err := d.sql.ExecContext(ctx,
			`INSERT INTO tags (name, team_id) VALUES (?, ?)
			 ON CONFLICT(team_id, name) DO NOTHING`,
			name, teamID,
		); err != nil {
			return model.Tag{}, err
		}
		return d.GetTagByName(ctx, teamID, name)
	}
	if _, err := d.sql.ExecContext(ctx,
		`INSERT OR IGNORE INTO tags (name) VALUES (?)`, name,
	); err != nil {
		return model.Tag{}, err
	}
	return d.GetTagByName(ctx, 0, name)
}

// GetTagByName returns the tag with the given name in the given team.
// Inputs are lowercased so callers don't need to normalise; the
// underlying column is COLLATE NOCASE so mixed-case input still
// resolves correctly. teamID == 0 skips the team filter.
func (d *DB) GetTagByName(ctx context.Context, teamID int64, name string) (model.Tag, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	q := `SELECT id, name, team_id, created_at FROM tags WHERE name = ?`
	args := []any{name}
	if teamID > 0 {
		q += ` AND team_id = ?`
		args = append(args, teamID)
	}
	row := d.sql.QueryRowContext(ctx, q, args...)
	return scanTag(row)
}

// ListTags returns every tag in the given team sorted alphabetically.
func (d *DB) ListTags(ctx context.Context, teamID int64) ([]model.Tag, error) {
	q := `SELECT id, name, team_id, created_at FROM tags`
	args := []any{}
	if teamID > 0 {
		q += ` WHERE team_id = ?`
		args = append(args, teamID)
	}
	q += ` ORDER BY name`
	rows, err := d.sql.QueryContext(ctx, q, args...)
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
// rows pointing to it. teamID > 0 restricts the delete to that workspace.
func (d *DB) DeleteTag(ctx context.Context, teamID, id int64) error {
	q := `DELETE FROM tags WHERE id = ?`
	args := []any{id}
	if teamID > 0 {
		q += ` AND team_id = ?`
		args = append(args, teamID)
	}
	res, err := d.sql.ExecContext(ctx, q, args...)
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
// preload the tag themselves. Pass 0 for teamID if the session is in
// no workspace (rare; usually Auth middleware supplies one).
func (d *DB) AttachTag(ctx context.Context, teamID, sessionID int64, tagName string) error {
	// Refuse to tag a session from another workspace.
	if teamID > 0 {
		if _, err := d.GetSession(ctx, teamID, sessionID); err != nil {
			return err
		}
	}
	tag, err := d.GetTagByName(ctx, teamID, tagName)
	if err != nil {
		// Auto-create on first attach — matches the "just type the tag"
		// UX of the web UI.
		if errors.Is(err, ErrTagNotFound) {
			var cerr error
			tag, cerr = d.CreateTag(ctx, teamID, tagName)
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
func (d *DB) DetachTag(ctx context.Context, teamID, sessionID int64, tagName string) error {
	if teamID > 0 {
		if _, err := d.GetSession(ctx, teamID, sessionID); err != nil {
			return err
		}
	}
	tag, err := d.GetTagByName(ctx, teamID, tagName)
	if err != nil {
		return err
	}
	_, err = d.sql.ExecContext(ctx,
		`DELETE FROM session_tags WHERE session_id = ? AND tag_id = ?`,
		sessionID, tag.ID)
	return err
}

// SetTagsForSession replaces the tag set for a session with the given
// names. Tags not yet in the catalogue are auto-created in teamID.
// Used by the stats inline-edit form where the user picks chips.
func (d *DB) SetTagsForSession(ctx context.Context, teamID, sessionID int64, tagNames []string) error {
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
		// Look up by name within this team.
		q := `SELECT id FROM tags WHERE name = ?`
		args := []any{name}
		if teamID > 0 {
			q += ` AND team_id = ?`
			args = append(args, teamID)
		}
		if err := tx.QueryRowContext(ctx, q, args...).Scan(&tagID); err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			// Create.
			res, err := tx.ExecContext(ctx,
				`INSERT INTO tags (name, team_id) VALUES (?, ?)`,
				name, nullableInt64(teamID),
			)
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
		SELECT t.id, t.name, t.team_id, t.created_at
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
	q := `SELECT st.session_id, t.id, t.name, t.team_id, t.created_at
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
			teamID    sql.NullInt64
			createdAt string
		)
		if err := rows.Scan(&sid, &t.ID, &t.Name, &teamID, &createdAt); err != nil {
			return nil, err
		}
		if teamID.Valid {
			t.TeamID = teamID.Int64
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
func (d *DB) ListSessionsByTag(ctx context.Context, teamID int64, tagName string) ([]model.ActiveSession, error) {
	tag, err := d.GetTagByName(ctx, teamID, tagName)
	if err != nil {
		return nil, err
	}
	q := sessionSelect + `
		JOIN session_tags st ON st.session_id = s.id
		WHERE s.end_at IS NOT NULL
		  AND st.tag_id = ?`
	args := []any{tag.ID}
	if teamID > 0 {
		q += ` AND s.team_id = ?`
		args = append(args, teamID)
	}
	q += ` ORDER BY s.start_at DESC`
	rows, err := d.sql.QueryContext(ctx, q, args...)
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

func (d *DB) ListAllTagsWithCounts(ctx context.Context, teamID int64) ([]TagWithCount, error) {
	q := `
		SELECT t.id, t.name, t.team_id, t.created_at, COUNT(st.session_id)
		FROM tags t
		LEFT JOIN session_tags st ON st.tag_id = t.id`
	args := []any{}
	if teamID > 0 {
		q += ` WHERE t.team_id = ?`
		args = append(args, teamID)
	}
	q += `
		GROUP BY t.id
		ORDER BY COUNT(st.session_id) DESC, t.name`
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TagWithCount
	for rows.Next() {
		var (
			tc        TagWithCount
			teamID    sql.NullInt64
			createdAt string
		)
		if err := rows.Scan(&tc.ID, &tc.Name, &teamID, &createdAt, &tc.SessionCount); err != nil {
			return nil, err
		}
		if teamID.Valid {
			tc.TeamID = teamID.Int64
		}
		if ts, err := ScanTime(createdAt); err == nil {
			tc.CreatedAt = ts
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}

func scanTag(r row) (model.Tag, error) {
	var (
		t         model.Tag
		teamID    sql.NullInt64
		createdAt string
	)
	if err := r.Scan(&t.ID, &t.Name, &teamID, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Tag{}, ErrTagNotFound
		}
		return model.Tag{}, err
	}
	if teamID.Valid {
		t.TeamID = teamID.Int64
	}
	if ts, err := ScanTime(createdAt); err == nil {
		t.CreatedAt = ts
	}
	return t, nil
}