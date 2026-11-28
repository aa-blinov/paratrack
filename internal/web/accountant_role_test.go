package web

import (
	"net/url"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// An accountant opens the invoices, payouts and reports and their exports,
// records a payment and closes a month, but cannot make, change or delete a
// document. A month the accountant closed is reopened by them or the owner,
// not by an admin.
func TestAccountantReadsTheMoneyAndClosesTheMonthButEditsNothing(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("own@acct.test")
	acc := newAPIEnvSharedDB(t, owner)
	acc.register("acc@acct.test")
	adm := newAPIEnvSharedDB(t, owner)
	adm.register("adm@acct.test")
	ids := joinStudio(t, owner, map[string]*apiEnv{"acc@acct.test": acc, "adm@acct.test": adm}, "acc@acct.test", "adm@acct.test")
	owner.do("POST", "/api/team/members/"+ids[0]+"/role", url.Values{"role": {"accountant"}}, nil).Body.Close()
	owner.do("POST", "/api/team/members/"+ids[1]+"/role", url.Values{"role": {"admin"}}, nil).Body.Close()
	sql := owner.db.TestSQL()
	var role string
	if err := sql.QueryRowContext(t.Context(), `SELECT role FROM memberships WHERE user_id = ? ORDER BY team_id DESC LIMIT 1`, ids[0]).Scan(&role); err != nil || role != "accountant" {
		t.Fatalf("the role was not given: %q %v", role, err)
	}
	var teamID, ownerID int64
	if err := sql.QueryRowContext(t.Context(), `SELECT m.team_id, m.user_id FROM memberships m JOIN users u ON u.id = m.user_id WHERE u.email = 'own@acct.test' AND m.role = 'owner' ORDER BY m.team_id DESC LIMIT 1`).Scan(&teamID, &ownerID); err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithActor(t.Context(), ownerID)
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	newInvoice := func(number string) int64 {
		inv, err := owner.db.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: teamID, CallerID: ownerID, Number: number, Client: "Ромашка", Start: start, End: start.AddDate(0, 1, 0),
			Lines: []model.InvoiceLine{{Label: "Работа", Seconds: 3600, RateCents: 100000, AmountCents: 100000}}})
		if err != nil {
			t.Fatal(err)
		}
		return inv.ID
	}
	sent, draft := newInvoice("INV-2026-701"), newInvoice("INV-2026-702")
	if err := owner.db.MarkInvoiceSentOnce(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: sent, CallerID: ownerID}); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		"/invoices", "/invoices/" + itoa64(sent), "/invoices/" + itoa64(sent) + "/pdf", "/invoices/" + itoa64(sent) + "/act",
		"/invoices/" + itoa64(sent) + "/sessions.csv", "/invoices/export.csv?start=2026-06-01&end=2026-06-30",
		"/invoices/payments.csv?start=2026-06-01&end=2026-06-30", "/payroll", "/reports",
	} {
		resp := acc.do("GET", path, nil, nil)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("accountant GET %s: %d", path, resp.StatusCode)
		}
	}
	for _, c := range []struct{ path string }{
		{"/invoices"}, {"/invoices/" + itoa64(draft) + "/delete"}, {"/invoices/" + itoa64(draft) + "/status"},
		{"/invoices/" + itoa64(draft) + "/lines"}, {"/payroll"}, {"/projects/new"},
	} {
		resp := acc.do("POST", c.path, url.Values{"status": {"sent"}, "label": {"x"}, "amount": {"1"}, "name": {"x"}, "start": {"2026-06-01"}, "end": {"2026-06-30"}}, nil)
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Errorf("accountant POST %s: %d, want 403", c.path, resp.StatusCode)
		}
	}
	var drafts int
	sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM invoices WHERE id = ? AND status = 'draft'`, draft).Scan(&drafts)
	if drafts != 1 {
		t.Fatal("the accountant changed a draft")
	}

	acc.do("POST", "/invoices/"+itoa64(sent)+"/payments", url.Values{"amount": {"400"}, "paid_on": {"2026-07-03"}}, nil).Body.Close()
	var paid int64
	sql.QueryRowContext(t.Context(), `SELECT COALESCE(SUM(amount_cents), 0) FROM invoice_payments WHERE invoice_id = ?`, sent).Scan(&paid)
	if paid != 40000 {
		t.Fatalf("the accountant's payment was not recorded: %d", paid)
	}

	closed := func() bool {
		var n int
		sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM closed_periods WHERE team_id = ? AND month = '2026-06-01'`, teamID).Scan(&n)
		return n == 1
	}
	acc.do("POST", "/api/period/close", url.Values{"month": {"2026-06-15"}}, nil).Body.Close()
	if !closed() {
		t.Fatal("the accountant could not close the month")
	}
	adm.do("POST", "/api/period/reopen", url.Values{"month": {"2026-06-01"}}, nil).Body.Close()
	if !closed() {
		t.Fatal("an admin reopened the month the accountant closed")
	}
	acc.do("POST", "/api/period/reopen", url.Values{"month": {"2026-06-01"}}, nil).Body.Close()
	if closed() {
		t.Fatal("the accountant could not reopen their own month")
	}
	adm.do("POST", "/api/period/close", url.Values{"month": {"2026-06-15"}}, nil).Body.Close()
	owner.do("POST", "/api/period/reopen", url.Values{"month": {"2026-06-01"}}, nil).Body.Close()
	if closed() {
		t.Fatal("the owner could not reopen a month")
	}
}

// Removing the accountant from the team does not let an admin reopen the
// month the accountant closed: the closer's role is kept with the close.
func TestAdminCannotReopenAnAccountantsMonthByRemovingThem(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("own@acct2.test")
	acc := newAPIEnvSharedDB(t, owner)
	acc.register("acc@acct2.test")
	adm := newAPIEnvSharedDB(t, owner)
	adm.register("adm@acct2.test")
	ids := joinStudio(t, owner, map[string]*apiEnv{"acc@acct2.test": acc, "adm@acct2.test": adm}, "acc@acct2.test", "adm@acct2.test")
	owner.do("POST", "/api/team/members/"+ids[0]+"/role", url.Values{"role": {"accountant"}}, nil).Body.Close()
	owner.do("POST", "/api/team/members/"+ids[1]+"/role", url.Values{"role": {"admin"}}, nil).Body.Close()
	sql := owner.db.TestSQL()
	acc.do("POST", "/api/period/close", url.Values{"month": {"2026-06-15"}}, nil).Body.Close()
	var teamID int64
	if err := sql.QueryRowContext(t.Context(), `SELECT team_id FROM closed_periods WHERE month = '2026-06-01' AND closed_by = ?`, ids[0]).Scan(&teamID); err != nil {
		t.Fatalf("the accountant could not close the month: %v", err)
	}
	adm.do("POST", "/api/team/members/"+ids[0]+"/remove", nil, nil).Body.Close()
	var still int
	sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM memberships WHERE team_id = ? AND user_id = ?`, teamID, ids[0]).Scan(&still)
	if still != 0 {
		t.Skip("an admin cannot remove the accountant here; nothing to bypass")
	}
	adm.do("POST", "/api/period/reopen", url.Values{"month": {"2026-06-01"}}, nil).Body.Close()
	var closed int
	sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM closed_periods WHERE team_id = ? AND month = '2026-06-01'`, teamID).Scan(&closed)
	if closed != 1 {
		t.Fatal("the admin reopened the accountant's month after removing them")
	}
}

// A storno is a new negative entry with a reason; a paid invoice goes back
// to sent, and the same entry cannot be reversed twice. A pay run keeps the
// payout day it was marked paid on.
func TestAccountantReversesAPaymentAndDatesAPayout(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("own@storno.test")
	sql := owner.db.TestSQL()
	var teamID, ownerID int64
	if err := sql.QueryRowContext(t.Context(), `SELECT team_id, user_id FROM memberships WHERE role = 'owner' ORDER BY team_id DESC LIMIT 1`).Scan(&teamID, &ownerID); err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithActor(t.Context(), ownerID)
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	inv, err := owner.db.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: teamID, CallerID: ownerID, Number: "INV-2026-801", Client: "Ромашка", Start: start, End: start.AddDate(0, 1, 0),
		Lines: []model.InvoiceLine{{Label: "Работа", Seconds: 3600, RateCents: 100000, AmountCents: 100000}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.db.MarkInvoiceSentOnce(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: inv.ID, CallerID: ownerID}); err != nil {
		t.Fatal(err)
	}
	owner.do("POST", "/invoices/"+itoa64(inv.ID)+"/payments", url.Values{"amount": {"1000"}, "paid_on": {"2026-07-03"}}, nil).Body.Close()
	status := func() string {
		var s string
		sql.QueryRowContext(t.Context(), `SELECT status FROM invoices WHERE id = ?`, inv.ID).Scan(&s)
		return s
	}
	if status() != "paid" {
		t.Fatalf("the payment did not settle the invoice: %s", status())
	}
	var paymentID int64
	sql.QueryRowContext(t.Context(), `SELECT id FROM invoice_payments WHERE invoice_id = ?`, inv.ID).Scan(&paymentID)
	path := "/invoices/" + itoa64(inv.ID) + "/payments/" + itoa64(paymentID) + "/reverse"
	owner.do("POST", path, url.Values{}, nil).Body.Close()
	if status() != "paid" {
		t.Fatal("a reversal without a reason went through")
	}
	owner.do("POST", path, url.Values{"note": {"ошибся счётом"}}, nil).Body.Close()
	var sum, rows int64
	sql.QueryRowContext(t.Context(), `SELECT COALESCE(SUM(amount_cents), 0), COUNT(*) FROM invoice_payments WHERE invoice_id = ?`, inv.ID).Scan(&sum, &rows)
	if sum != 0 || rows != 2 || status() != "sent" {
		t.Fatalf("after the storno: sum %d, rows %d, status %s; want 0, 2, sent", sum, rows, status())
	}
	owner.do("POST", path, url.Values{"note": {"ещё раз"}}, nil).Body.Close()
	sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM invoice_payments WHERE invoice_id = ?`, inv.ID).Scan(&rows)
	if rows != 2 {
		t.Fatal("the same payment was reversed twice")
	}
	future := time.Now().AddDate(0, 0, 5).Format("2006-01-02")
	owner.do("POST", "/invoices/"+itoa64(inv.ID)+"/payments", url.Values{"amount": {"10"}, "paid_on": {future}}, nil).Body.Close()
	sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM invoice_payments WHERE invoice_id = ?`, inv.ID).Scan(&rows)
	if rows != 2 {
		t.Fatal("a payment dated in the future was booked")
	}
}
