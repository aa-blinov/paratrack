package web

import (
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// The payment form takes money as a person types it and books it into the journal
// (FIN-08). What is more than the invoice still owes, or is not a sum, books nothing.
func TestInvoicePaymentFormBooksTypedMoney(t *testing.T) {
	e := newAPIEnv(t)
	e.register("journal-owner@x.test")
	var ownerID, teamID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(),
		`SELECT u.id, m.team_id FROM users u JOIN memberships m ON m.user_id = u.id WHERE u.email = 'journal-owner@x.test'`).
		Scan(&ownerID, &teamID); err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithActor(t.Context(), ownerID)
	start := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	inv, err := e.db.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: teamID, CallerID: ownerID, Number: "INV-FORM-1", Client: "Client", Start: start, End: start.AddDate(0, 0, 1),
		Lines: []model.InvoiceLine{{Label: "Работа", Seconds: 7200, RateCents: 10000, AmountCents: 20000}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.db.MarkInvoiceSentOnce(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: inv.ID, CallerID: ownerID}); err != nil {
		t.Fatal(err)
	}
	booked := func() int64 {
		t.Helper()
		var sum int64
		if err := e.db.TestSQL().QueryRowContext(t.Context(),
			`SELECT COALESCE(SUM(amount_cents), 0) FROM invoice_payments WHERE invoice_id = ?`, inv.ID).Scan(&sum); err != nil {
			t.Fatal(err)
		}
		return sum
	}
	post := func(amount string) {
		t.Helper()
		resp := e.do("POST", fmt.Sprintf("/invoices/%d/payments", inv.ID), url.Values{
			"amount": {amount}, "paid_on": {"2026-09-10"}, "note": {"перевод"},
		}, nil)
		resp.Body.Close()
		if resp.StatusCode != 303 {
			t.Fatalf("payment %q: status %d, want a redirect back to the invoice", amount, resp.StatusCode)
		}
	}

	post("150,50")
	if got := booked(); got != 15050 {
		t.Fatalf("typed 150,50: journal holds %d cents, want 15050", got)
	}
	post("300")
	post("abc")
	if got := booked(); got != 15050 {
		t.Fatalf("refused amounts changed the journal: %d cents, want 15050", got)
	}
	post("49,50")
	if got := booked(); got != 20000 {
		t.Fatalf("the rest of the invoice: journal holds %d cents, want 20000", got)
	}
	var status string
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT status FROM invoices WHERE id = ?`, inv.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "paid" {
		t.Fatalf("status after the payments cover the total = %q, want paid", status)
	}
}
