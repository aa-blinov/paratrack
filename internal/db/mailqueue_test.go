package db

import (
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/mailport"
)

func TestTruncateUTF8(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		maxBytes int
		want     string
	}{
		{name: "within limit", value: "готово", maxBytes: 20, want: "готово"},
		{name: "zero limit", value: "текст", maxBytes: 0, want: ""},
		{name: "boundary in multibyte rune", value: strings.Repeat("a", 1999) + "💾", maxBytes: 2000, want: strings.Repeat("a", 1999)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateUTF8(tt.value, tt.maxBytes); got != tt.want {
				t.Fatalf("truncateUTF8() length = %d, want %d", len(got), len(tt.want))
			}
		})
	}
}

// The delivered letter is the only copy of the files an invoice went out
// with: resending repeats it, so delivery keeps it and a failed attempt is
// never mistaken for it.
func TestDeliveredLetterStaysForResend(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Resend", "resend")
	ownerID := teamOwner(t, d, teamID)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	invoice, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{
		TeamID: teamID, CallerID: ownerID, Number: "INV-RESEND-1", Client: "Client",
		Start: start, End: start.AddDate(0, 0, 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	now := FormatTime(d.currentTime().UTC())
	payload := `{"version":1,"message":{"to":"client@example.test","subject":"Invoice","text":"body"}}`
	var outboxID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO invoice_email_outbox (team_id, invoice_id, recipient, payload, status, attempts, available_at, locked_at, created_at)
		 VALUES (?, ?, ?, ?, 'processing', 1, ?, ?, ?) RETURNING id`,
		teamID, invoice.ID, "client@example.test", payload, now, now, now).Scan(&outboxID); err != nil {
		t.Fatal(err)
	}
	if err := d.CompleteInvoiceEmail(ctx, mailport.InvoiceEmailJob{
		ID: outboxID, TeamID: teamID, InvoiceID: invoice.ID, Attempts: 1, Lease: now,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := d.LastInvoiceLetter(ctx, appmodel.InvoiceLetterQuery{TeamID: teamID, InvoiceID: invoice.ID})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != payload {
		t.Fatalf("delivered letter = %q, want %q", got, payload)
	}
	// A later failed send keeps its own payload, and that one never went out.
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO invoice_email_outbox (team_id, invoice_id, recipient, payload, status, attempts, available_at, created_at)
		 VALUES (?, ?, ?, ?, 'failed', 1, ?, ?)`,
		teamID, invoice.ID, "old@example.test", `{"version":1,"message":{"to":"old@example.test","subject":"Lost","text":"lost"}}`, now, now); err != nil {
		t.Fatal(err)
	}
	got, err = d.LastInvoiceLetter(ctx, appmodel.InvoiceLetterQuery{TeamID: teamID, InvoiceID: invoice.ID})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != payload {
		t.Fatalf("letter after a failed send = %q, want the delivered %q", got, payload)
	}
}
