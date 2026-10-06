package db

import (
	"context"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// Audit persistence

type AuditEntry = model.AuditEntry

// Audit records an event. action examples: "auth.login", "project.delete",
// "invoice.create". teamID/userID may be 0 for pre-auth events.

func (d *DB) Audit(ctx context.Context, record model.AuditRecord) error {
	_, err := d.sql.ExecContext(ctx,
		`INSERT INTO audit_log (team_id, user_id, action, target, meta, ip, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		record.TeamID, record.UserID, record.Action, record.Target, record.Meta, record.IP, FormatTime(d.currentTime().UTC()))
	return err
}

// ListAudit returns the team's audit trail, newest first. Unset filters keep
// the whole trail. Offset lets a wider window extend a list the reader already
// has instead of restarting it, so a shared link reopens the same events.
// Event times are compared as text: the column stores UTC RFC3339Nano, and
// FormatTime writes the same shape, so the team index still serves the range.
func (d *DB) ListAudit(ctx context.Context, query appmodel.AuditListQuery) ([]AuditEntry, error) {
	if query.TeamID <= 0 {
		return nil, ErrNotFound
	}
	if query.Limit <= 0 || query.Limit > 500 {
		query.Limit = 100
	}
	if query.Offset < 0 {
		query.Offset = 0
	}
	statement := `SELECT id, team_id, user_id, action, target, meta, ip, created_at FROM audit_log WHERE team_id = ?`
	args := []any{query.TeamID}
	if !query.From.IsZero() {
		statement += ` AND created_at >= ?`
		args = append(args, FormatTime(query.From))
	}
	if !query.To.IsZero() {
		statement += ` AND created_at < ?`
		args = append(args, FormatTime(query.To))
	}
	if query.UserID > 0 {
		statement += ` AND user_id = ?`
		args = append(args, query.UserID)
	}
	if query.Action != "" {
		statement += ` AND action = ?`
		args = append(args, query.Action)
	}
	statement += ` ORDER BY id DESC LIMIT ? OFFSET ?`
	args = append(args, query.Limit, query.Offset)
	rows, err := d.sql.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var created string
		if err := rows.Scan(&e.ID, &e.TeamID, &e.UserID, &e.Action, &e.Target, &e.Meta, &e.IP, &created); err != nil {
			return nil, err
		}
		e.CreatedAt, err = ScanTime(created)
		if err != nil {
			return nil, fmt.Errorf("parse audit event time: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
