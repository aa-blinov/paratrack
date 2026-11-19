package db

import (
	"context"
	"fmt"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// RecurringWorkspaces lists the workspaces with the schedule on (UX-20) — the
// owner acts on the schedule's behalf, the zone keeps the months' bounds.
func (d *DB) RecurringWorkspaces(ctx context.Context) ([]appmodel.RecurringWorkspace, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, owner_id, timezone FROM teams WHERE recurring_invoices = '1st'`)
	if err != nil {
		return nil, fmt.Errorf("list workspaces with recurring invoices: %w", err)
	}
	defer rows.Close()
	var out []appmodel.RecurringWorkspace
	for rows.Next() {
		var w appmodel.RecurringWorkspace
		var zone string
		if err := rows.Scan(&w.TeamID, &w.OwnerID, &zone); err != nil {
			return nil, err
		}
		w.Zone = time.UTC
		if zone != "" {
			if loc, err := time.LoadLocation(zone); err == nil {
				w.Zone = loc
			}
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ClaimRecurringInvoiceLetter marks the ready-to-issue letter for this month as
// due. It answers true exactly once, so a pass that retried an earlier crash
// creates the missing drafts again (the billing stamps make repeats a no-op)
// without anyone hearing twice.
func (d *DB) ClaimRecurringInvoiceLetter(ctx context.Context, request appmodel.RecurringLetterClaimRequest) (bool, error) {
	if request.TeamID <= 0 || request.Month == "" {
		return false, nil
	}
	now := FormatTime(d.currentTime().UTC())
	result, err := d.sql.ExecContext(ctx,
		`INSERT INTO recurring_invoice_letters (team_id, month, created_at) VALUES (?, ?, ?)
		 ON CONFLICT (team_id, month) DO NOTHING`,
		request.TeamID, request.Month, now)
	if err != nil {
		return false, fmt.Errorf("claim recurring invoice letter for team %d, %s: %w", request.TeamID, request.Month, err)
	}
	if rows, err := result.RowsAffected(); err != nil {
		return false, err
	} else {
		return rows == 1, nil
	}
}
