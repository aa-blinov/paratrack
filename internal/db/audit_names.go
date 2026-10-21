package db

import (
	"context"
	"fmt"
	"strings"
)

// ListAuditActors lists every user who left a row in the team's journal. Current
// members and former ones alike: an account outlives its membership, so the
// journal can still name someone who has left.
func (d *DB) ListAuditActors(ctx context.Context, teamID int64) ([]int64, error) {
	if teamID <= 0 {
		return nil, ErrNotFound
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT DISTINCT user_id FROM audit_log WHERE team_id = ? AND user_id > 0 ORDER BY user_id`, teamID)
	if err != nil {
		return nil, fmt.Errorf("list audit actors: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// UserLabels names users for the journal: the display name, or the address when no
// name is set, the same fallback the member list uses. An id with no account is
// left out of the result.
func (d *DB) UserLabels(ctx context.Context, ids []int64) (map[int64]string, error) {
	labels := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return labels, nil
	}
	marks := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		marks[i] = "?"
		args[i] = id
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, name, email FROM users WHERE id IN (`+strings.Join(marks, ", ")+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("name audit users: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name, email string
		if err := rows.Scan(&id, &name, &email); err != nil {
			return nil, err
		}
		label := strings.TrimSpace(name)
		if label == "" {
			label = email
		}
		labels[id] = label
	}
	return labels, rows.Err()
}
