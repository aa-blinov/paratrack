package db

import (
	"context"
	"database/sql"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// ListInvoiceSessions returns the closed sessions billed on one invoice, oldest first
// (FIN-06). A billed session points at its invoice, so this is exactly what the invoice
// charges for, by session rather than by the summed lines.
func (d *DB) ListInvoiceSessions(ctx context.Context, query appmodel.InvoiceLookupQuery) ([]appmodel.InvoiceSessionRow, error) {
	if query.TeamID <= 0 || query.InvoiceID <= 0 {
		return nil, ErrNotFound
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT COALESCE(u.name, ''), COALESCE(p.name, ''), a.name,
		        s.start_at, s.end_at, s.accumulated_seconds, s.paused, s.last_resume_at
		   FROM sessions s
		   JOIN activities a ON a.id = s.activity_id AND a.team_id = s.team_id
		   LEFT JOIN users u ON u.id = s.user_id
		   LEFT JOIN projects p ON p.id = `+sessionProject+`
		  WHERE s.team_id = ? AND s.invoice_id = ? AND s.end_at IS NOT NULL
		  ORDER BY s.start_at, s.id`, query.TeamID, query.InvoiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []appmodel.InvoiceSessionRow
	for rows.Next() {
		var row appmodel.InvoiceSessionRow
		var startAt, endAt string
		var lastResumeAt sql.NullString
		var accumulated, paused int
		if err := rows.Scan(&row.Person, &row.Project, &row.Activity, &startAt, &endAt, &accumulated, &paused, &lastResumeAt); err != nil {
			return nil, err
		}
		session, err := scheduleSession(startAt, sql.NullString{String: endAt, Valid: true}, lastResumeAt, accumulated, paused)
		if err != nil {
			return nil, err
		}
		row.Start = session.StartAt
		row.End = *session.EndAt
		row.Seconds = session.DurationSeconds(row.End)
		out = append(out, row)
	}
	return out, rows.Err()
}
