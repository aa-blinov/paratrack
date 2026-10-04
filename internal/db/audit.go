package db

import (
	"context"
	"fmt"

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

// ListAudit returns the team's audit trail, newest first.
func (d *DB) ListAudit(ctx context.Context, teamID int64, limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, team_id, user_id, action, target, meta, ip, created_at
		 FROM audit_log WHERE team_id = ? ORDER BY id DESC LIMIT ?`, teamID, limit)
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
