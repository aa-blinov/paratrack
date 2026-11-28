package web

import (
	"net/url"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// A closed month locks the drafts left in it, keeps the zone that bounds it,
// a storno is dated the day of the payment it reverses, and a payout marked
// by mistake goes back to a draft with a reason.
func TestClosedMonthLocksDraftsAndBooksHold(t *testing.T) {
	e := newAPIEnv(t)
	e.register("closed-drafts@x.test")
	sql := e.db.TestSQL()
	var teamID, ownerID int64
	if err := sql.QueryRowContext(t.Context(), `SELECT team_id, user_id FROM memberships WHERE role = 'owner' ORDER BY team_id DESC LIMIT 1`).Scan(&teamID, &ownerID); err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithActor(t.Context(), ownerID)
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	newInvoice := func(number string) int64 {
		inv, err := e.db.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: teamID, CallerID: ownerID, Number: number, Client: "Ромашка", Start: start, End: start.AddDate(0, 1, 0),
			Lines: []model.InvoiceLine{{Label: "Работа", Seconds: 3600, RateCents: 100000, AmountCents: 100000}}})
		if err != nil {
			t.Fatal(err)
		}
		return inv.ID
	}
	draft, sent := newInvoice("INV-2026-951"), newInvoice("INV-2026-952")
	if err := e.db.MarkInvoiceSentOnce(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: sent, CallerID: ownerID}); err != nil {
		t.Fatal(err)
	}
	e.do("POST", "/invoices/"+itoa64(sent)+"/payments", url.Values{"amount": {"300"}, "paid_on": {"2026-07-02"}}, nil).Body.Close()
	var paymentID int64
	sql.QueryRowContext(t.Context(), `SELECT id FROM invoice_payments WHERE invoice_id = ?`, sent).Scan(&paymentID)
	e.do("POST", "/invoices/"+itoa64(sent)+"/payments/"+itoa64(paymentID)+"/reverse", url.Values{"note": {"не тот счёт"}}, nil).Body.Close()
	var reversedOn string
	sql.QueryRowContext(t.Context(), `SELECT paid_on FROM invoice_payments WHERE invoice_id = ? AND source = 'reversal'`, sent).Scan(&reversedOn)
	if reversedOn != "2026-07-02" {
		t.Fatalf("the storno is dated %q, want the payment's day", reversedOn)
	}

	// A month with a draft left in it does not close.
	e.do("POST", "/api/period/close", url.Values{"month": {"2026-06-15"}}, nil).Body.Close()
	var closedCount int
	sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM closed_periods WHERE team_id = ?`, teamID).Scan(&closedCount)
	if closedCount != 0 {
		t.Fatal("a month with a draft in it closed")
	}
	// A month closed before that rule, with a draft in it, still locks it.
	if _, err := sql.ExecContext(t.Context(), `INSERT INTO closed_periods (team_id, month, closed_at, closed_by, closed_by_role) VALUES (?, '2026-06-01', ?, ?, 'owner')`, teamID, time.Now().UTC().Format(time.RFC3339), ownerID); err != nil {
		t.Fatal(err)
	}
	lines := func() int {
		var n int
		sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM invoice_lines WHERE invoice_id = ?`, draft).Scan(&n)
		return n
	}
	e.do("POST", "/invoices/"+itoa64(draft)+"/lines", url.Values{"label": {"Доплата"}, "amount": {"100"}}, nil).Body.Close()
	if lines() != 1 {
		t.Fatal("a line was added to a draft in a closed month")
	}
	e.do("POST", "/invoices/"+itoa64(draft)+"/status", url.Values{"status": {"sent"}}, nil).Body.Close()
	e.do("POST", "/invoices/"+itoa64(draft)+"/paid", nil, nil).Body.Close()
	e.do("POST", "/invoices/"+itoa64(draft)+"/delete", nil, nil).Body.Close()
	var status string
	sql.QueryRowContext(t.Context(), `SELECT status FROM invoices WHERE id = ?`, draft).Scan(&status)
	if status != "draft" {
		t.Fatalf("a draft in a closed month was issued, paid or deleted: %q", status)
	}
	e.do("POST", "/api/team/calendar", url.Values{"timezone": {"Asia/Yekaterinburg"}, "week_start": {"1"}}, nil).Body.Close()
	var zone string
	sql.QueryRowContext(t.Context(), `SELECT COALESCE(timezone, '') FROM teams WHERE id = ?`, teamID).Scan(&zone)
	if zone == "Asia/Yekaterinburg" {
		t.Fatal("the zone moved while a month was closed")
	}
}
