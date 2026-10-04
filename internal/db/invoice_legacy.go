package db

import (
	"context"
	"fmt"
)

const billedCutoff = "2026-09-27T09:05:00"

// assignOrphanSessions gives sessions recorded before user_id was set on
// every path (backfill, timesheet, import, API) to the workspace owner,
// the only person who could have made them in a personal workspace and
// the least-wrong owner in a shared one. This historical backfill runs once.
func (d *DB) assignOrphanSessions(ctx context.Context) error {
	var n int64
	applied, err := d.runDataMigration(ctx, "20260927_assign_orphan_session_users", func(tx *Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE sessions SET user_id = (SELECT owner_id FROM teams WHERE teams.id = sessions.team_id)
			 WHERE user_id IS NULL AND team_id IS NOT NULL`)
		if err != nil {
			return err
		}
		n, err = res.RowsAffected()
		return err
	})
	if err != nil {
		return err
	}
	if applied && n > 0 {
		d.logger.Printf("sessions: %d without an author assigned to their workspace owner", n)
	}
	return nil
}

func (d *DB) stampLegacyInvoices(ctx context.Context) error {
	var n int64
	applied, err := d.runDataMigration(ctx, "20260927_stamp_legacy_invoice_sessions", func(tx *Tx) error {
		res, err := tx.ExecContext(ctx, `
		UPDATE sessions SET invoice_id = (
			SELECT i.id FROM invoices i
			JOIN invoice_lines l ON l.invoice_id = i.id
			JOIN activities a ON a.id = sessions.activity_id
			JOIN projects p ON p.id = a.project_id
			WHERE i.team_id = sessions.team_id AND a.team_id = sessions.team_id AND p.team_id = sessions.team_id AND i.created_at < ?
			  AND sessions.start_at >= i.period_start AND sessions.start_at < i.period_end
			  AND sessions.created_at <= i.created_at
			  AND l.label = p.name || ' · ' || a.name
			ORDER BY i.id LIMIT 1)
		WHERE invoice_id IS NULL AND end_at IS NOT NULL AND EXISTS (
			SELECT 1 FROM invoices i
			JOIN invoice_lines l ON l.invoice_id = i.id
			JOIN activities a ON a.id = sessions.activity_id
			JOIN projects p ON p.id = a.project_id
			WHERE i.team_id = sessions.team_id AND a.team_id = sessions.team_id AND p.team_id = sessions.team_id AND i.created_at < ?
			  AND sessions.start_at >= i.period_start AND sessions.start_at < i.period_end
			  AND sessions.created_at <= i.created_at
			  AND l.label = p.name || ' · ' || a.name)`, billedCutoff, billedCutoff)
		if err != nil {
			return fmt.Errorf("stamp legacy billed sessions: %w", err)
		}
		n, err = res.RowsAffected()
		if err != nil {
			return fmt.Errorf("count stamped legacy sessions: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if applied && n > 0 {
		d.logger.Printf("invoices: stamped %d sessions billed before invoice_id existed", n)
	}
	return nil
}
