package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// ClientTags lists a client's tags by name. Every member of the workspace reads them,
// the same as the client card they sit on.
func (d *DB) ClientTags(ctx context.Context, query appmodel.ClientScopeQuery) ([]string, error) {
	if query.TeamID <= 0 || query.ClientID <= 0 {
		return nil, ErrNotFound
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT name FROM client_tags WHERE team_id = ? AND client_id = ? ORDER BY name`,
		query.TeamID, query.ClientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// SetClientTags replaces the tag set of one client. A member is refused in the
// transaction, before the tags are read, so the answer does not depend on what the
// member sent. The client row is locked so two sets written at once cannot interleave.
func (d *DB) SetClientTags(ctx context.Context, request appmodel.ClientTagsRequest) ([]string, error) {
	if request.TeamID <= 0 || request.ClientID <= 0 || request.CallerID <= 0 {
		return nil, ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return nil, err
	}
	tags, err := model.NormalizeClientTags(request.Tags)
	if err != nil {
		return nil, err
	}
	var locked int64
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM clients WHERE id = ? AND team_id = ? FOR UPDATE`, request.ClientID, request.TeamID).Scan(&locked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM client_tags WHERE team_id = ? AND client_id = ?`, request.TeamID, request.ClientID); err != nil {
		return nil, err
	}
	now := FormatTime(d.currentTime().UTC())
	for _, tag := range tags {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO client_tags (team_id, client_id, name, created_by, created_at) VALUES (?, ?, ?, ?, ?)`,
			request.TeamID, request.ClientID, tag, request.CallerID, now); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return tags, nil
}
