package db

import (
	"context"
)

// TrackedSecondsForInvoice sums the tracked time of the sessions on one invoice,
// with the same duration rule the unbilled totals use. A draft's lines are a
// snapshot, so this sum is what the invoice should show now.
func (d *DB) TrackedSecondsForInvoice(ctx context.Context, teamID, invoiceID int64) (int, error) {
	var total int64
	err := d.sql.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(CASE WHEN s.accumulated_seconds > 0 THEN s.accumulated_seconds
		                          ELSE GREATEST(0, FLOOR(EXTRACT(EPOCH FROM (s.end_at::timestamp - s.start_at::timestamp)))) END), 0)::bigint
		 FROM sessions s
		 WHERE s.team_id = ? AND s.invoice_id = ? AND s.end_at IS NOT NULL`,
		teamID, invoiceID).Scan(&total)
	return int(total), err
}
