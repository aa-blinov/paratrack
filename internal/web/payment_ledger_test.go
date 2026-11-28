package web

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// The ledger lists payments by the day they were paid, with the client's
// ИНН read from the invoice's details, in the 1С dialect when asked.
func TestPaymentLedgerCSV(t *testing.T) {
	rows := []appmodel.PaymentLedgerRow{
		{PaidOn: "2026-11-03", InvoiceNumber: "INV-2026-007", Client: "ООО «Ромашка»", ClientINN: "7707083893", AmountCents: 500050, Currency: "RUB", Source: "manual", Note: "аванс"},
	}
	var excel strings.Builder
	if err := writePaymentLedgerCSV(&excel, rows, csvDialect{excel: true}); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimPrefix(excel.String(), "\\ufeff")
	if !strings.Contains(got, "paid_on;invoice;client;client_inn;amount") || !strings.Contains(got, "2026-11-03;INV-2026-007;ООО «Ромашка»;7707083893;5000,50;RUB;manual;аванс") {
		t.Fatalf("ledger:\\n%s", got)
	}
}

// Through the route: a payment recorded on a sent invoice is in the ledger
// for its month and not outside it.
func TestPaymentLedgerRoute(t *testing.T) {
	e := newAPIEnv(t)
	e.register("ledger@x.test")
	e.do("POST", "/api/team/modules", url.Values{"preset": {"freelance"}, "from": {"welcome"}}, nil).Body.Close()
	var teamID, ownerID int64
	if err := e.db.TestSQL().QueryRowContext(t.Context(), `SELECT m.team_id, u.id FROM memberships m JOIN users u ON u.id = m.user_id WHERE u.email = 'ledger@x.test'`).Scan(&teamID, &ownerID); err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithActor(t.Context(), ownerID)
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	inv, err := e.db.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: teamID, CallerID: ownerID, Number: "INV-2026-501", Client: "Ромашка", Start: start, End: start.AddDate(0, 1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.TestSQL().ExecContext(ctx, `UPDATE invoices SET client_details = 'ООО «Ромашка», ИНН 7707083893' WHERE id = ?`, inv.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.db.AddInvoiceLine(ctx, appmodel.InvoiceLineAddRequest{TeamID: teamID, InvoiceID: inv.ID, CallerID: ownerID, Label: "Работа", AmountCents: 100000}); err != nil {
		t.Fatal(err)
	}
	if err := e.db.MarkInvoiceSentOnce(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: inv.ID, CallerID: ownerID}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.RecordInvoicePayment(ctx, appmodel.InvoicePaymentRequest{TeamID: teamID, InvoiceID: inv.ID, CallerID: ownerID, PaidOn: "2026-07-03", AmountCents: 40000}); err != nil {
		t.Fatal(err)
	}
	july := readBody(t, e.do("GET", "/invoices/payments.csv?export_from=2026-07-01&export_to=2026-07-31", nil, nil))
	if !strings.Contains(july, "2026-07-03,INV-2026-501,Ромашка,7707083893,400.00,RUB,manual") {
		t.Fatalf("July ledger:\n%s", july)
	}
	june := readBody(t, e.do("GET", "/invoices/payments.csv?export_from=2026-06-01&export_to=2026-06-30", nil, nil))
	if strings.Contains(june, "INV-2026-501") {
		t.Fatalf("a July payment in the June ledger:\n%s", june)
	}
}
