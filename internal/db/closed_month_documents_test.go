package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// closedMonthFixture is one draft invoice inside June 2026 with a line and
// valid requisites, ready to be sent.
func closedMonthFixture(t *testing.T) (*DB, int64, int64, model.Invoice) {
	t.Helper()
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Закрытый месяц", "closed-documents")
	ownerID := teamOwner(t, d, teamID)
	ctx = requestctx.WithActor(ctx, ownerID)
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	invoice, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{
		TeamID: teamID, CallerID: ownerID, Number: "INV-2026-700", Client: "Client",
		Start: start, End: start.AddDate(0, 1, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetTeamRequisites(ctx, appmodel.TeamRequisitesRequest{TeamID: teamID, CallerID: ownerID, Requisites: "ИП Иванов, ИНН 7707083893"}); err != nil {
		t.Fatalf("save requisites: %v", err)
	}
	if err := d.AddInvoiceLine(ctx, appmodel.InvoiceLineAddRequest{TeamID: teamID, InvoiceID: invoice.ID, CallerID: ownerID, Label: "Работа", AmountCents: 100000}); err != nil {
		t.Fatal(err)
	}
	return d, teamID, ownerID, invoice
}

// An invoice whose letter is already on its way still becomes sent, so the
// month it belongs to cannot close under it.
func TestMonthWithALetterOnItsWayDoesNotClose(t *testing.T) {
	d, teamID, ownerID, invoice := closedMonthFixture(t)
	ctx := requestctx.WithActor(t.Context(), ownerID)
	current, err := d.GetInvoice(ctx, appmodel.InvoiceLookupQuery{TeamID: teamID, InvoiceID: invoice.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.EnqueueInvoiceEmail(ctx, appmodel.InvoiceEmailEnqueueRequest{
		TeamID: teamID, InvoiceID: invoice.ID, Revision: current.Revision,
		Recipient: "client@example.test", Payload: []byte(`{"version":1,"message":{"to":"client@example.test","subject":"Счёт","text":"во вложении"}}`),
	}); err != nil {
		t.Fatalf("queue the letter: %v", err)
	}
	if err := d.CloseMonth(ctx, appmodel.PeriodCloseRequest{TeamID: teamID, CallerID: ownerID, Month: "2026-06-01"}); !errors.Is(err, model.ErrMonthHasDrafts) {
		t.Fatalf("closing June under a letter on its way: err = %v, want drafts", err)
	}
}

// A cancelled document stays cancelled: it takes no receipt number and no
// money, whether the payment is typed in or arrives from the provider. The
// manual path said so, the automatic one did not.
func TestCancelledInvoiceTakesNoReceiptAndNoMoney(t *testing.T) {
	d, teamID, ownerID, invoice := closedMonthFixture(t)
	ctx := requestctx.WithActor(t.Context(), ownerID)
	if err := d.MarkInvoiceSentOnce(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: invoice.ID, CallerID: ownerID}); err != nil {
		t.Fatal(err)
	}
	if err := d.VoidInvoice(ctx, appmodel.InvoiceVoidRequest{TeamID: teamID, InvoiceID: invoice.ID, CallerID: ownerID, Reason: "выставлен по ошибке"}); err != nil {
		t.Fatalf("void the invoice: %v", err)
	}
	if err := d.SetInvoiceReceipt(ctx, appmodel.InvoiceReceiptRequest{TeamID: teamID, InvoiceID: invoice.ID, CallerID: ownerID, Receipt: "receipt-1"}); err == nil {
		t.Fatal("a cancelled invoice took a receipt number")
	}
	if _, err := d.MarkInvoicePaidFromStripe(ctx, appmodel.InvoiceStripePaymentRequest{
		TeamID: teamID, InvoiceID: invoice.ID, StripeSessionID: "cs_test_void",
	}); !errors.Is(err, model.ErrInvoiceNotPayable) {
		t.Fatalf("a cancelled invoice paid from the provider: err = %v, want not payable", err)
	}
}

// A payment entered by mistake is reversed, never deleted, and a handed-over
// month takes neither the reversal nor a moved zone: both would rewrite what
// the accountant closed.
func TestReversalAndZoneFollowTheClosedMonth(t *testing.T) {
	d, teamID, ownerID, invoice := closedMonthFixture(t)
	ctx := requestctx.WithActor(t.Context(), ownerID)
	if err := d.MarkInvoiceSentOnce(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: invoice.ID, CallerID: ownerID}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.RecordInvoicePayment(ctx, appmodel.InvoicePaymentRequest{
		TeamID: teamID, InvoiceID: invoice.ID, CallerID: ownerID, PaidOn: "2026-06-15", AmountCents: 100000,
	}); err != nil {
		t.Fatalf("record a payment: %v", err)
	}
	var paymentID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT id FROM invoice_payments WHERE invoice_id = ? ORDER BY id LIMIT 1`, invoice.ID).Scan(&paymentID); err != nil {
		t.Fatal(err)
	}
	if err := d.CloseMonth(ctx, appmodel.PeriodCloseRequest{TeamID: teamID, CallerID: ownerID, Month: "2026-06-01"}); err != nil {
		t.Fatalf("close June: %v", err)
	}
	if err := d.ReverseInvoicePayment(ctx, appmodel.InvoicePaymentReverseRequest{
		TeamID: teamID, InvoiceID: invoice.ID, PaymentID: paymentID, CallerID: ownerID, Note: "оплата по ошибке",
	}); err == nil {
		t.Fatal("a reversal landed in a closed month")
	}
	if err := d.SetTeamCalendar(ctx, appmodel.TeamCalendarRequest{TeamID: teamID, CallerID: ownerID, TimeZone: "Asia/Novosibirsk"}); !errors.Is(err, model.ErrZoneWithClosedMonths) {
		t.Fatalf("moving the zone with a closed month: err = %v, want zone lock", err)
	}
	if err := d.ReopenMonth(ctx, appmodel.PeriodCloseRequest{TeamID: teamID, CallerID: ownerID, Month: "2026-06-01"}); err != nil {
		t.Fatalf("reopen June: %v", err)
	}
	if err := d.ReverseInvoicePayment(ctx, appmodel.InvoicePaymentReverseRequest{
		TeamID: teamID, InvoiceID: invoice.ID, PaymentID: paymentID, CallerID: ownerID, Note: "оплата по ошибке",
	}); err != nil {
		t.Fatalf("a reversal in an open month: %v", err)
	}
	paid, err := d.GetInvoice(ctx, appmodel.InvoiceLookupQuery{TeamID: teamID, InvoiceID: invoice.ID})
	if err != nil {
		t.Fatal(err)
	}
	if paid.Status != "sent" {
		t.Fatalf("a reversed invoice reads %q, want sent", paid.Status)
	}
	if err := d.ReverseInvoicePayment(ctx, appmodel.InvoicePaymentReverseRequest{
		TeamID: teamID, InvoiceID: invoice.ID, PaymentID: paymentID, CallerID: ownerID, Note: "ещё раз",
	}); err == nil {
		t.Fatal("the same payment was reversed twice")
	}
}
