package db

import (
	"errors"
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestEnqueueInvoiceEmail_RechecksManagerRole(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Mail queue auth", "mail-queue-auth")
	ownerID := teamOwner(t, d, teamID)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	invoice, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: teamID, CallerID: ownerID, Number: "INV-EMAIL-001", Client: "Client", Start: start, End: start.AddDate(0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	memberID := seedProjectTestUser(t, d, "mail-queue-member")
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, memberID, FormatTime(start)); err != nil {
		t.Fatal(err)
	}

	if err := d.EnqueueInvoiceEmail(requestctx.WithActor(ctx, memberID), appmodel.InvoiceEmailEnqueueRequest{
		TeamID: teamID, InvoiceID: invoice.ID, Revision: invoice.Revision,
		Recipient: "client@example.test", Payload: []byte(`{"message":"frozen"}`),
	}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("member enqueue error = %v, want forbidden", err)
	}
	got, err := d.GetInvoice(ctx, appmodel.InvoiceLookupQuery{TeamID: teamID, InvoiceID: invoice.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "draft" {
		t.Fatalf("unauthorized enqueue changed invoice status to %q", got.Status)
	}
	var queued int
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT count(*) FROM invoice_email_outbox WHERE invoice_id = ?`, invoice.ID).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued != 0 {
		t.Fatalf("unauthorized enqueue created %d outbox rows", queued)
	}
}
