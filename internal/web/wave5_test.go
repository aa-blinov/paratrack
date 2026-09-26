package web

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/db"
)

func TestInvoicePDFAndPayment(t *testing.T) {
	d, err := newTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	d.SQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`)
	d.SQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)
	p, err := d.CreateProject(ctx, 1, "Acme", "", "#7c3aed")
	if err != nil {
		t.Fatal(err)
	}
	rate := 10000
	if err := d.SetProjectRate(ctx, 1, p.ID, &rate, nil); err != nil {
		t.Fatal(err)
	}
	act, _ := d.CreateActivity(ctx, 1, "consulting")
	_ = d.AssignActivityProject(ctx, 1, act.ID, p.ID)
	start := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	d.CreateClosedSession(ctx, 1, act.ID, start, end, "")

	num, _ := d.NextInvoiceNumber(ctx, 1)
	inv, err := d.CreateInvoice(ctx, 1, num, "Acme Corp", start, end, "net 14", []db.InvoiceLine{{
		Label: "Acme · consulting", Seconds: 7200, RateCents: 10000, AmountCents: 20000,
	}})
	if err != nil {
		t.Fatal(err)
	}

	// PDF bytes
	vm := invoiceVM{
		ID: inv.ID, Number: inv.Number, ClientName: inv.ClientName,
		PeriodLabel: "Sep 21, 2026 – Sep 22, 2026",
		Notes: "net 14",
		Lines: []invoiceLineVM{{
			Label: "Acme · consulting", Hours: "2h", Rate: "100.00", Amount: "200.00",
			Seconds: 7200, RateCents: 10000, AmountCents: 20000,
		}},
		Total: "200.00", TotalCents: 20000, Hours: "2h",
	}
	pdf, err := renderInvoicePDF(vm, "My Team")
	if err != nil {
		t.Fatalf("pdf: %v", err)
	}
	if len(pdf) < 500 {
		t.Fatalf("pdf too small: %d", len(pdf))
	}
	if !strings.HasPrefix(string(pdf), "%PDF") {
		t.Fatalf("not a pdf: %q", string(pdf[:10]))
	}
	if invoicePDFName(vm) != inv.Number+".pdf" {
		t.Errorf("name=%s", invoicePDFName(vm))
	}

	// payment URL save + mark paid
	if err := d.SetPaymentURL(ctx, 1, inv.ID, "https://buy.stripe.com/xyz", "cs_1"); err != nil {
		t.Fatal(err)
	}
	got, _ := d.GetInvoice(ctx, 1, inv.ID)
	if got.PaymentURL != "https://buy.stripe.com/xyz" {
		t.Fatalf("payment=%s", got.PaymentURL)
	}
	if err := d.MarkInvoicePaid(ctx, 1, inv.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = d.GetInvoice(ctx, 1, inv.ID)
	if got.Status != "paid" {
		t.Fatalf("status=%s", got.Status)
	}
}

func TestManualPaymentLinkEndpoint(t *testing.T) {
	e := newAPIEnv(t)
	e.register("pay@x.test")
	// seed a tiny invoice via DB through API is heavy — use form create
	start := "2026-09-20"
	end := "2026-09-27"
	// no billable time yet → create project+rate+session first via HTTP is verbose;
	// manual link path only needs an invoice, so generate via db on the same server.
	// Fall back: create through the UI form after seeding time with backfill.
	resp := e.do("POST", "/api/sessions/backfill", url.Values{
		"activity": {"consulting"},
		"start":    {"yesterday 09:00"},
		"end":      {"yesterday 11:00"},
	}, map[string]string{"HX-Request": "true"})
	resp.Body.Close()

	// give the project a rate (project_id 1 may not exist) — skip if create fails
	resp = e.do("POST", "/projects/new", url.Values{"name": {"Acme"}, "color": {"#7c3aed"}}, nil)
	resp.Body.Close()
	resp = e.do("POST", "/projects/acme", url.Values{
		"name": {"Acme"}, "color": {"#7c3aed"}, "rate_cents": {"10000"}, "billable": {"1"},
	}, nil)
	resp.Body.Close()

	// Unassigned time is not invoiceable — bind consulting to Acme first.
	act, err := e.srv.db.GetOrCreateActivity(t.Context(), 1, "consulting")
	if err != nil {
		t.Fatalf("activity: %v", err)
	}
	p, err := e.srv.db.GetProjectBySlug(t.Context(), 1, "acme")
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if err := e.srv.db.AssignActivityProject(t.Context(), 1, act.ID, p.ID); err != nil {
		t.Fatalf("assign: %v", err)
	}

	resp = e.do("POST", "/invoices", url.Values{
		"client": {"Acme"}, "start": {start}, "end": {end},
	}, nil)
	if resp.StatusCode != 303 {
		t.Fatalf("invoice: %d %s", resp.StatusCode, readBody(t, resp))
	}
	loc := resp.Header.Get("Location")
	resp.Body.Close()
	if !strings.HasPrefix(loc, "/invoices/") {
		t.Fatalf("loc=%s", loc)
	}
	// PDF
	resp = e.do("GET", loc+"/pdf", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("pdf: %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.HasPrefix(body, "%PDF") {
		t.Fatalf("not pdf")
	}
	// manual payment link
	resp = e.do("POST", loc+"/pay", url.Values{
		"mode": {"manual"}, "url": {"https://pay.example.com/x"},
	}, nil)
	if resp.StatusCode != 303 {
		t.Fatalf("pay: %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = e.do("GET", loc, nil, nil)
	page := readBody(t, resp)
	if !strings.Contains(page, "pay.example.com") {
		t.Fatalf("payment url missing in page")
	}
}
