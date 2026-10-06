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

// ErrTagNotFound is returned when a tag is missing.
var ErrTagNotFound = model.ErrTagNotFound

// createTag inserts or resolves a tag in the legacy unscoped catalog.
// Workspace writes must use CreateTagForMember.

func (d *DB) createTag(ctx context.Context, request legacyTagCreateRequest) (model.Tag, error) {
	teamID, name := request.TeamID, request.Name
	if teamID != 0 {
		return model.Tag{}, model.ErrForbidden
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return model.Tag{}, fmt.Errorf("tag name cannot be empty")
	}
	if _, err := d.sql.ExecContext(ctx,
		`INSERT INTO tags (name) VALUES (?) ON CONFLICT DO NOTHING`, name,
	); err != nil {
		return model.Tag{}, err
	}
	return d.getUnscopedTagByName(ctx, name)
}

func (d *DB) getUnscopedTagByName(ctx context.Context, name string) (model.Tag, error) {
	return scanTag(d.sql.QueryRowContext(ctx,
		`SELECT id, name, team_id, created_at FROM tags WHERE name = ? AND team_id IS NULL`, name))
}

// CreateTagForMember creates or resolves a tag for an authenticated workspace
// member, serializing the write with member removal.
func (d *DB) CreateTagForMember(ctx context.Context, request appmodel.TagCreateRequest) (model.Tag, error) {
	name := strings.ToLower(strings.TrimSpace(request.Name))
	if name == "" {
		return model.Tag{}, fmt.Errorf("tag name cannot be empty")
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.Tag{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockCurrentTeamMember(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return model.Tag{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO tags (name, team_id) VALUES (?, ?) ON CONFLICT(team_id, name) DO NOTHING`, name, request.TeamID); err != nil {
		return model.Tag{}, err
	}
	tag, err := scanTag(tx.QueryRowContext(ctx,
		`SELECT id, name, team_id, created_at FROM tags WHERE name = ? AND team_id = ?`, name, request.TeamID))
	if err != nil {
		return model.Tag{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Tag{}, err
	}
	return tag, nil
}

// ListTags returns every tag in the given team sorted alphabetically.
func (d *DB) ListTags(ctx context.Context, query appmodel.TagListQuery) ([]model.Tag, error) {
	if query.TeamID <= 0 {
		return nil, ErrNotFound
	}
	q := `SELECT id, name, team_id, created_at FROM tags`
	args := []any{}
	if query.TeamID > 0 {
		q += ` WHERE team_id = ?`
		args = append(args, query.TeamID)
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

// deleteTag removes a tag from the legacy unscoped catalog. Workspace writes
// must use DeleteTagForManager.

func (d *DB) deleteTag(ctx context.Context, request legacyTagDeleteRequest) error {
	teamID, id := request.TeamID, request.TagID
	if teamID != 0 {
		return model.ErrForbidden
	}
	res, err := d.sql.ExecContext(ctx, `DELETE FROM tags WHERE id = ? AND team_id IS NULL`, id)
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

// DeleteTagForManager serializes a destructive manager action with membership
// changes, so a concurrently demoted member cannot delete workspace data.
func (d *DB) DeleteTagForManager(ctx context.Context, request appmodel.TagDeleteRequest) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return err
	}
	if err := execRequireRows(ctx, tx, `DELETE FROM tags WHERE id = ? AND team_id = ?`, request.TagID, request.TeamID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrTagNotFound
		}
		return err
	}
	return tx.Commit()
}

// RenameTagForManager relabels a workspace tag without touching its row
// identity, so every session already linked through session_tags keeps the
// tag under its new name. A name another tag already owns is refused instead of
// collapsing both labels into one.
func (d *DB) RenameTagForManager(ctx context.Context, request appmodel.TagRenameRequest) (model.Tag, error) {
	name := strings.ToLower(strings.TrimSpace(request.Name))
	if name == "" {
		return model.Tag{}, fmt.Errorf("tag name cannot be empty")
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.Tag{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return model.Tag{}, err
	}
	if err := lockTagForRename(ctx, tx, request.TeamID, request.TagID); err != nil {
		return model.Tag{}, err
	}
	var previousName string
	if err := tx.QueryRowContext(ctx,
		`SELECT name FROM tags WHERE id = ? AND team_id = ?`, request.TagID, request.TeamID).Scan(&previousName); err != nil {
		return model.Tag{}, err
	}
	var otherID int64
	switch err := tx.QueryRowContext(ctx,
		`SELECT id FROM tags WHERE team_id = ? AND name = ? AND id <> ?`, request.TeamID, name, request.TagID).Scan(&otherID); {
	case err == nil:
		return model.Tag{}, appmodel.ErrTagNameTaken
	case !errors.Is(err, sql.ErrNoRows):
		return model.Tag{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE tags SET name = ? WHERE id = ? AND team_id = ?`, name, request.TagID, request.TeamID); err != nil {
		if isUniqueViolation(err) {
			// A concurrent rename claimed the name between the check and
			// the update; report it the same way as a direct collision.
			return model.Tag{}, appmodel.ErrTagNameTaken
		}
		return model.Tag{}, err
	}
	tag, err := scanTag(tx.QueryRowContext(ctx,
		`SELECT id, name, team_id, created_at FROM tags WHERE id = ? AND team_id = ?`, request.TagID, request.TeamID))
	if err != nil {
		return model.Tag{}, err
	}
	// Saved stats presets remember a tag by name, so they have to follow the
	// rename or the preset silently starts showing an empty filter.
	if _, err := tx.ExecContext(ctx,
		`UPDATE saved_reports SET tag = ? WHERE team_id = ? AND tag = ?`, name, request.TeamID, previousName,
	); err != nil {
		return model.Tag{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Tag{}, err
	}
	return tag, nil
}

// lockTagForRename freezes the tag row so two managers renaming the same tag
// cannot interleave, and so a tag deleted mid-flight is reported as missing
// rather than silently recreated by the update.
func lockTagForRename(ctx context.Context, tx *Tx, teamID, tagID int64) error {
	var id int64
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM tags WHERE id = ? AND team_id = ? FOR UPDATE`, tagID, teamID).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrTagNotFound
		}
		return err
	}
	return nil
}

// attachTag links a tag in the legacy unscoped catalog. Workspace writes must
// use AttachTagForMember.

func (d *DB) attachTag(ctx context.Context, request legacySessionTagRequest) error {
	teamID, sessionID, tagName := request.TeamID, request.SessionID, request.Name
	tagName = strings.ToLower(strings.TrimSpace(tagName))
	if tagName == "" {
		return fmt.Errorf("tag name cannot be empty")
	}
	if teamID != 0 {
		return model.ErrForbidden
	}
	tag, err := d.getUnscopedTagByName(ctx, tagName)
	if errors.Is(err, ErrTagNotFound) {
		tag, err = d.createTag(ctx, legacyTagCreateRequest{Name: tagName})
	}
	if err != nil {
		return err
	}
	_, err = d.sql.ExecContext(ctx, `INSERT INTO session_tags (session_id, tag_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, sessionID, tag.ID)
	return err
}

// AttachTagForMember attaches a tag after serializing against workspace member
// removal and rechecking the actor's current membership.
func (d *DB) AttachTagForMember(ctx context.Context, request appmodel.SessionTagRequest) error {
	tagName := strings.ToLower(strings.TrimSpace(request.Name))
	if tagName == "" {
		return fmt.Errorf("tag name cannot be empty")
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockCurrentTeamMember(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return err
	}
	if err := lockSessionForTagging(ctx, tx, request.TeamID, request.SessionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO tags (name, team_id) VALUES (?, ?) ON CONFLICT(team_id, name) DO NOTHING`, tagName, request.TeamID); err != nil {
		return err
	}
	var tagID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM tags WHERE team_id = ? AND name = ?`, request.TeamID, tagName).Scan(&tagID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO session_tags (session_id, tag_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, request.SessionID, tagID); err != nil {
		return err
	}
	return tx.Commit()
}

// DetachTagForMember removes a tag association after a transactional current
// membership check under the workspace lock.
func (d *DB) DetachTagForMember(ctx context.Context, request appmodel.SessionTagRequest) error {
	tagName := strings.ToLower(strings.TrimSpace(request.Name))
	if tagName == "" {
		return fmt.Errorf("tag name cannot be empty")
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockCurrentTeamMember(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return err
	}
	if err := lockSessionForTagging(ctx, tx, request.TeamID, request.SessionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM session_tags st USING tags t WHERE st.session_id = ? AND st.tag_id = t.id AND t.team_id = ? AND t.name = ?`, request.SessionID, request.TeamID, tagName); err != nil {
		return err
	}
	return tx.Commit()
}

// detachTag removes a link in the legacy unscoped catalog. Workspace writes
// must use DetachTagForMember.

func (d *DB) detachTag(ctx context.Context, request legacySessionTagRequest) error {
	teamID, sessionID, tagName := request.TeamID, request.SessionID, request.Name
	tagName = strings.ToLower(strings.TrimSpace(tagName))
	if tagName == "" {
		return fmt.Errorf("tag name cannot be empty")
	}
	if teamID != 0 {
		return model.ErrForbidden
	}
	tag, err := d.getUnscopedTagByName(ctx, tagName)
	if err != nil {
		return err
	}
	_, err = d.sql.ExecContext(ctx, `DELETE FROM session_tags WHERE session_id = ? AND tag_id = ?`, sessionID, tag.ID)
	return err
}

func lockSessionForTagging(ctx context.Context, tx *Tx, teamID, sessionID int64) error {
	var id int64
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM sessions WHERE id = ? AND team_id = ? FOR UPDATE`, sessionID, teamID).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// setTagsForSession replaces tags on a session in the legacy unscoped catalog.
// Workspace writes must use the actor-aware member operations.

func (d *DB) setTagsForSession(ctx context.Context, request legacySessionTagsRequest) error {
	teamID, sessionID, tagNames := request.TeamID, request.SessionID, request.Names
	if teamID != 0 {
		return model.ErrForbidden
	}
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
		if err := tx.QueryRowContext(ctx, `SELECT id FROM tags WHERE name = ? AND team_id IS NULL`, name).Scan(&tagID); err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			// Create.
			if err := tx.QueryRowContext(ctx,
				`INSERT INTO tags (name) VALUES (?) RETURNING id`,
				name,
			).Scan(&tagID); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO session_tags (session_id, tag_id) VALUES (?, ?) ON CONFLICT DO NOTHING`,
			sessionID, tagID,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// listTagsForSession returns every tag attached to the given session,
// ordered alphabetically.
func (d *DB) listTagsForSession(ctx context.Context, teamID, sessionID int64) ([]model.Tag, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT t.id, t.name, t.team_id, t.created_at
		FROM sessions s
		JOIN session_tags st ON st.session_id = s.id
		JOIN tags t ON t.id = st.tag_id
		WHERE COALESCE(s.team_id, 0) = ? AND s.id = ?
		  AND (t.team_id IS NULL OR t.team_id = s.team_id)
		ORDER BY t.name`, teamID, sessionID)
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
func (d *DB) TagsForSessions(ctx context.Context, query appmodel.SessionTagsQuery) (map[int64][]model.Tag, error) {
	if query.TeamID < 0 {
		return nil, ErrNotFound
	}
	for _, id := range query.SessionIDs {
		if id <= 0 {
			return nil, ErrNotFound
		}
	}
	out := make(map[int64][]model.Tag, len(query.SessionIDs))
	if len(query.SessionIDs) == 0 {
		return out, nil
	}
	// One array parameter: an IN list of ?s hits Postgres' 65535-parameter
	// cap on a big team's year.
	q := `SELECT st.session_id, t.id, t.name, t.team_id, t.created_at
	      FROM session_tags st
	      JOIN sessions s ON s.id = st.session_id
	      JOIN tags t ON t.id = st.tag_id
	      WHERE COALESCE(s.team_id, 0) = ? AND st.session_id = ANY(?)
	        AND (t.team_id IS NULL OR t.team_id = s.team_id)
	      ORDER BY st.session_id, t.name`
	rows, err := d.sql.QueryContext(ctx, q, query.TeamID, query.SessionIDs)
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
		t.CreatedAt, err = ScanTime(createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse tag creation time: %w", err)
		}
		out[sid] = append(out[sid], t)
	}
	return out, rows.Err()
}

// ListAllTagsWithCounts returns every tag with the number of sessions
// (active + closed) that carry it. Used by the /tags page.
type TagWithCount = appmodel.TagWithCount

func (d *DB) ListAllTagsWithCounts(ctx context.Context, query appmodel.TagListQuery) ([]TagWithCount, error) {
	if query.TeamID <= 0 {
		return nil, ErrNotFound
	}
	q := `
		SELECT t.id, t.name, t.team_id, t.created_at, COUNT(s.id)
		FROM tags t
		LEFT JOIN session_tags st ON st.tag_id = t.id
		LEFT JOIN sessions s ON s.id = st.session_id AND s.team_id = t.team_id
		WHERE t.team_id = ?`
	args := []any{query.TeamID}
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
		tc.CreatedAt, err = ScanTime(createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse tag creation time: %w", err)
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
	created, err := ScanTime(createdAt)
	if err != nil {
		return model.Tag{}, fmt.Errorf("parse tag creation time: %w", err)
	}
	t.CreatedAt = created
	return t, nil
}
