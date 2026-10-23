package db

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/money"
)

// ReconcileClients compares, for each client, the time worked on its projects in a window
// with what the invoices bill for it (FIN-04). Billed time is the lines of the client's
// issued invoices whose period overlaps the window; drafts are left out. Not-billed time
// is worked time whose session no invoice holds.
func (d *DB) ReconcileClients(ctx context.Context, query appmodel.ReconcileQuery) ([]appmodel.ClientReconciliation, error) {
	if query.TeamID <= 0 || !query.Until.After(query.From) {
		return nil, ErrNotFound
	}
	clientRows, err := d.sql.QueryContext(ctx, `SELECT id, name FROM clients WHERE team_id = ? AND archived = 0`, query.TeamID)
	if err != nil {
		return nil, err
	}
	byID := map[int64]*appmodel.ClientReconciliation{}
	for clientRows.Next() {
		var id int64
		var name string
		if err := clientRows.Scan(&id, &name); err != nil {
			clientRows.Close()
			return nil, err
		}
		byID[id] = &appmodel.ClientReconciliation{ClientName: name}
	}
	if err := clientRows.Err(); err != nil {
		clientRows.Close()
		return nil, err
	}
	clientRows.Close()

	sessionRows, err := d.sql.QueryContext(ctx,
		`SELECT s.invoice_id, p.client_id, s.start_at, s.end_at, s.accumulated_seconds, s.paused, s.last_resume_at
		   FROM sessions s
		   JOIN activities a ON a.id = s.activity_id AND a.team_id = s.team_id
		   JOIN projects p ON p.id = `+sessionProject+` AND p.team_id = s.team_id
		  WHERE s.team_id = ? AND p.client_id IS NOT NULL AND s.start_at < ? AND (s.end_at IS NULL OR s.end_at > ?)`,
		query.TeamID, FormatTime(query.Until), FormatTime(query.From))
	if err != nil {
		return nil, fmt.Errorf("read sessions for the reconciliation: %w", err)
	}
	defer sessionRows.Close()
	for sessionRows.Next() {
		var (
			invoiceID           sql.NullInt64
			clientID            int64
			startAt             string
			endAt, lastResumeAt sql.NullString
			accumulated, paused int
		)
		if err := sessionRows.Scan(&invoiceID, &clientID, &startAt, &endAt, &accumulated, &paused, &lastResumeAt); err != nil {
			return nil, err
		}
		client, ok := byID[clientID]
		if !ok {
			continue
		}
		session, err := scheduleSession(startAt, endAt, lastResumeAt, accumulated, paused)
		if err != nil {
			return nil, err
		}
		seconds := session.TrackedSecondsInWindow(query.From, query.Until, query.Now)
		client.WorkedSeconds += seconds
		if !invoiceID.Valid {
			client.NotBilledSeconds += seconds
		}
	}
	if err := sessionRows.Err(); err != nil {
		return nil, err
	}

	billedRows, err := d.sql.QueryContext(ctx,
		`SELECT i.client_name, COALESCE(SUM(l.seconds), 0)
		   FROM invoice_lines l JOIN invoices i ON i.id = l.invoice_id
		  WHERE i.team_id = ? AND i.status <> 'draft' AND i.period_start < ? AND i.period_end > ?
		  GROUP BY i.client_name`,
		query.TeamID, FormatTime(query.Until), FormatTime(query.From))
	if err != nil {
		return nil, fmt.Errorf("read invoice lines for the reconciliation: %w", err)
	}
	defer billedRows.Close()
	for billedRows.Next() {
		var name string
		var seconds int64
		if err := billedRows.Scan(&name, &seconds); err != nil {
			return nil, err
		}
		for _, client := range byID {
			if strings.EqualFold(client.ClientName, name) {
				total, err := money.AddInt(client.BilledSeconds, int(seconds))
				if err != nil {
					return nil, fmt.Errorf("sum billed time: %w", err)
				}
				client.BilledSeconds = total
			}
		}
	}
	if err := billedRows.Err(); err != nil {
		return nil, err
	}

	out := make([]appmodel.ClientReconciliation, 0, len(byID))
	for _, client := range byID {
		client.DiffSeconds = client.WorkedSeconds - client.BilledSeconds - client.NotBilledSeconds
		if client.WorkedSeconds == 0 && client.BilledSeconds == 0 {
			continue
		}
		out = append(out, *client)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ClientName < out[j].ClientName })
	return out, nil
}
