package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// A sent invoice takes payments into its journal until they cover it (FIN-08).
// A partial payment leaves the invoice open, an over-payment is refused, and a
// draft or a paid invoice takes none.
func TestInvoiceJournalSettlesOnlyWhenPaymentsCoverTheTotal(t *testing.T) {
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Журнал", "payment-journal")
	ownerID := teamOwner(t, d, teamID)
	ctx := requestctx.WithActor(t.Context(), ownerID)
	start := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	lines := []model.InvoiceLine{{Label: "Работа", Seconds: 7200, RateCents: 10000, AmountCents: 20000}}
	create := func(number string) model.Invoice {
		t.Helper()
		inv, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: teamID, CallerID: ownerID, Number: number, Client: "Client", Start: start, End: start.AddDate(0, 0, 1), Lines: lines})
		if err != nil {
			t.Fatal(err)
		}
		return inv
	}
	sent := create("INV-JOURNAL-1")
	if err := d.MarkInvoiceSentOnce(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: sent.ID, CallerID: ownerID}); err != nil {
		t.Fatal(err)
	}
	pay := func(invoiceID int64, cents int, paidOn string) (bool, error) {
		t.Helper()
		return d.RecordInvoicePayment(ctx, appmodel.InvoicePaymentRequest{TeamID: teamID, InvoiceID: invoiceID, CallerID: ownerID, PaidOn: paidOn, AmountCents: cents, Note: "перевод"})
	}

	if settled, err := pay(sent.ID, 5000, "2026-09-10"); err != nil || settled {
		t.Fatalf("partial payment: settled %v, err %v; want an open invoice", settled, err)
	}
	if _, err := pay(sent.ID, 15001, "2026-09-11"); !errors.Is(err, model.ErrPaymentAmount) {
		t.Fatalf("over-payment: err %v, want ErrPaymentAmount", err)
	}
	details, err := d.GetInvoiceDetails(ctx, appmodel.InvoiceLookupQuery{TeamID: teamID, InvoiceID: sent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(details.Payments) != 1 || details.Payments[0].AmountCents != 5000 || details.Payments[0].PaidOn != "2026-09-10" || details.Payments[0].Note != "перевод" {
		t.Fatalf("journal = %+v, want the one partial entry", details.Payments)
	}
	if details.Invoice.Status != "sent" {
		t.Fatalf("status after a partial payment = %q, want sent", details.Invoice.Status)
	}

	if settled, err := pay(sent.ID, 15000, "2026-09-20"); err != nil || !settled {
		t.Fatalf("last payment: settled %v, err %v; want the invoice paid", settled, err)
	}
	details, err = d.GetInvoiceDetails(ctx, appmodel.InvoiceLookupQuery{TeamID: teamID, InvoiceID: sent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if details.Invoice.Status != "paid" || len(details.Payments) != 2 {
		t.Fatalf("status %q with %d entries, want paid with two entries", details.Invoice.Status, len(details.Payments))
	}
	if _, err := pay(sent.ID, 100, "2026-09-21"); !errors.Is(err, model.ErrInvoiceNotPayable) {
		t.Fatalf("payment on a paid invoice: err %v, want ErrInvoiceNotPayable", err)
	}

	draft := create("INV-JOURNAL-2")
	if _, err := pay(draft.ID, 100, "2026-09-21"); !errors.Is(err, model.ErrInvoiceNotPayable) {
		t.Fatalf("payment on a draft: err %v, want ErrInvoiceNotPayable", err)
	}
}

// Marking an invoice paid books what is still owed, so the journal always adds
// up to the total (FIN-08).
func TestMarkingPaidBooksWhatIsStillOwed(t *testing.T) {
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Остаток", "payment-remainder")
	ownerID := teamOwner(t, d, teamID)
	ctx := requestctx.WithActor(t.Context(), ownerID)
	start := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	inv, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: teamID, CallerID: ownerID, Number: "INV-REMAINDER", Client: "Client", Start: start, End: start.AddDate(0, 0, 1),
		Lines: []model.InvoiceLine{{Label: "Работа", Seconds: 7200, RateCents: 10000, AmountCents: 20000}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.MarkInvoiceSentOnce(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: inv.ID, CallerID: ownerID}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.RecordInvoicePayment(ctx, appmodel.InvoicePaymentRequest{TeamID: teamID, InvoiceID: inv.ID, CallerID: ownerID, PaidOn: "2026-09-10", AmountCents: 8000}); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := d.MarkInvoicePaidOnce(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: inv.ID, CallerID: ownerID}); err != nil || !changed {
		t.Fatalf("mark paid: changed %v, err %v", changed, err)
	}
	details, err := d.GetInvoiceDetails(ctx, appmodel.InvoiceLookupQuery{TeamID: teamID, InvoiceID: inv.ID})
	if err != nil {
		t.Fatal(err)
	}
	sum := 0
	for _, p := range details.Payments {
		sum += p.AmountCents
	}
	if sum != 20000 || len(details.Payments) != 2 || details.Payments[1].AmountCents != 12000 {
		t.Fatalf("journal = %+v (sum %d), want the 80 booked and the 120 remainder, adding to 200", details.Payments, sum)
	}
}
