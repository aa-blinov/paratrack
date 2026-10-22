package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// The invoice's appendix lists the sessions billed on it, oldest first, and leaves out
// time that is not on this invoice (FIN-06).
func TestInvoiceAppendixListsOnlyItsBilledSessions(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Приложение", "invoice-appendix")
	ownerID := teamOwner(t, d, teamID)
	activity := newTestActivity(t, d, ctx, teamID, ownerID, "Вёрстка")

	day := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	later := seedClosedSession(t, d, teamID, ownerID, activity, day.Add(14*time.Hour))
	earlier := seedClosedSession(t, d, teamID, ownerID, activity, day.Add(9*time.Hour))
	seedClosedSession(t, d, teamID, ownerID, activity, day.Add(16*time.Hour)) // not on the invoice

	var invoiceID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO invoices (team_id, number, client_name, period_start, period_end, status, created_at)
		 VALUES (?, 'INV-APPENDIX', 'Client', ?, ?, 'sent', ?) RETURNING id`,
		teamID, FormatTime(day), FormatTime(day.AddDate(0, 0, 7)), FormatTime(time.Now().UTC())).Scan(&invoiceID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{later, earlier} {
		if _, err := d.TestSQL().ExecContext(ctx, `UPDATE sessions SET invoice_id = ? WHERE id = ?`, invoiceID, id); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := d.ListInvoiceSessions(ctx, appmodel.InvoiceLookupQuery{TeamID: teamID, InvoiceID: invoiceID})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("the appendix lists %d sessions, want the two billed ones", len(rows))
	}
	if !rows[0].Start.Equal(day.Add(9*time.Hour)) || !rows[1].Start.Equal(day.Add(14*time.Hour)) {
		t.Fatalf("the sessions are not oldest first: %v, %v", rows[0].Start, rows[1].Start)
	}
	for _, row := range rows {
		if row.Seconds != 3600 || row.Activity != "Вёрстка" || row.Person == "" {
			t.Fatalf("a billed session row is %+v, want an hour on Вёрстка with a person", row)
		}
	}
}
