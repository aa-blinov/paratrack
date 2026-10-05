package web

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestInvoicePDFAndPayment(t *testing.T) {
	d, err := newTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	d.TestSQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`)
	d.TestSQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)
	d.TestSQL().ExecContext(ctx, `INSERT INTO memberships (team_id, user_id, role) VALUES (1,1,'owner')`)
	ctx = requestctx.WithActor(ctx, 1)
	p, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: 1, CallerID: 1, Name: "Acme", Slug: "", Color: "#7c3aed"})
	if err != nil {
		t.Fatal(err)
	}
	rate := 10000
	if err := d.SetProjectRate(ctx, appmodel.ProjectRateRequest{TeamID: 1, ProjectID: p.ID, CallerID: 1, RateCents: &rate}); err != nil {
		t.Fatal(err)
	}
	act, _ := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: 1, CallerID: 1, Name: "consulting"})
	_ = d.AssignActivityProject(ctx, appmodel.AssignActivityProjectRequest{TeamID: 1, ActivityID: act.ID, ProjectID: p.ID, CallerID: 1})
	start := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: 1, ActivityID: act.ID, Start: start, End: end})

	num, _ := d.NextInvoiceNumber(ctx, 1)
	inv, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: 1, CallerID: 1, Number: num, Client: "Acme Corp", Start: start, End: end, Notes: "net 14", Lines: []db.InvoiceLine{{
		Label: "Acme · consulting", Seconds: 7200, RateCents: 10000, AmountCents: 20000,
	}}})
	if err != nil {
		t.Fatal(err)
	}

	// PDF bytes
	vm := invoiceVM{
		ID: inv.ID, Number: inv.Number, ClientName: inv.ClientName,
		PeriodLabel: "Sep 21, 2026 – Sep 22, 2026",
		Notes:       "оплата в течение 14 дней",
		Lines: []invoiceLineVM{{
			Label: "Acme · consulting", Hours: "2h", Rate: "100.00", Amount: "200.00",
			Seconds: 7200, RateCents: 10000, AmountCents: 20000,
		}},
		Total: "200.00", TotalCents: 20000, Hours: "2h",
	}
	pdf, err := renderInvoicePDF(vm, "Анна Фрилансер", i18n.Ru)
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
	if err := d.SetPaymentURL(ctx, appmodel.InvoicePaymentLinkSaveRequest{
		TeamID: 1, InvoiceID: inv.ID, CallerID: 1, ExpectedRevision: inv.Revision,
		PaymentURL: "https://buy.stripe.com/xyz", StripeSessionID: "cs_1",
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := d.GetInvoice(ctx, appmodel.InvoiceLookupQuery{TeamID: 1, InvoiceID: inv.ID})
	if got.PaymentURL != "https://buy.stripe.com/xyz" {
		t.Fatalf("payment=%s", got.PaymentURL)
	}
	if _, changed, err := d.MarkInvoicePaidOnce(ctx, appmodel.InvoiceMutationRequest{TeamID: 1, InvoiceID: inv.ID, CallerID: 1}); err != nil || !changed {
		t.Fatalf("mark paid = (changed=%v, err=%v), want first transition", changed, err)
	}
	got, _ = d.GetInvoice(ctx, appmodel.InvoiceLookupQuery{TeamID: 1, InvoiceID: inv.ID})
	if got.Status != "paid" {
		t.Fatalf("status=%s", got.Status)
	}
}

func TestManualPaymentLinkEndpoint(t *testing.T) {
	e := newAPIEnv(t)
	e.register("pay@x.test")
	// seed a tiny invoice via DB through API is heavy — use form create
	start := time.Now().AddDate(0, 0, -2).Format("2006-01-02")
	end := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
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
	act, err := e.db.GetOrCreateActivityForMember(t.Context(), appmodel.ActivityResolveRequest{TeamID: 1, CallerID: 1, Name: "consulting"})
	if err != nil {
		t.Fatalf("activity: %v", err)
	}
	p, err := e.db.GetProjectBySlug(t.Context(), appmodel.ProjectSlugQuery{TeamID: 1, Slug: "acme"})
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if err := e.db.AssignActivityProject(t.Context(), appmodel.AssignActivityProjectRequest{TeamID: 1, ActivityID: act.ID, ProjectID: p.ID, CallerID: 1}); err != nil {
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

// Issuer details and the VAT line are copied onto the invoice at creation,
// and the act renders for the same lines.
func TestInvoiceRequisitesSnapshotAndAct(t *testing.T) {
	e := newAPIEnv(t)
	e.register("act@x.test")
	readBody(t, e.do("POST", "/api/team/requisites", url.Values{"requisites": {"ИНН 770000000000"}, "vat_note": {"НДС не облагается"}}, nil))
	readBody(t, e.do("POST", "/projects/new", url.Values{"name": {"Ромашка"}, "rate": {"2 500,50"}}, nil))
	readBody(t, e.do("POST", "/api/sessions/backfill", url.Values{"activity": {"вёрстка"}, "start": {"вчера 10:00"}, "end": {"вчера 12:00"}, "project_id": {"1"}}, map[string]string{"HX-Request": "true"}))
	day := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	resp := e.do("POST", "/invoices", url.Values{"client": {"ООО «Ромашка»"}, "client_details": {"КПП 770001001"}, "start": {day}, "end": {day}}, nil)
	loc := resp.Header.Get("Location")
	resp.Body.Close()
	if !strings.HasPrefix(loc, "/invoices/") {
		t.Fatalf("create: %d %s", resp.StatusCode, loc)
	}
	readBody(t, e.do("POST", "/api/team/requisites", url.Values{"requisites": {"changed"}}, nil))
	page := readBody(t, e.do("GET", loc, nil, nil))
	for _, want := range []string{"ИНН 770000000000", "НДС не облагается", "КПП 770001001", "5\u00a0001,00"} {
		if !strings.Contains(page, want) {
			t.Errorf("invoice page lacks %q", want)
		}
	}
	if reactData[invoiceDetailPage](t, page).Seller != "U" {
		t.Fatal("invoice bootstrap lacks the personal workspace owner's name")
	}
	act := readBody(t, e.do("GET", loc+"/act", nil, nil))
	if !strings.Contains(act, `"InvoiceActReact":true`) {
		t.Error("act page did not bootstrap its React view")
	}
	if !reactData[invoiceDetailPage](t, act).InvoiceActReact {
		t.Error("act page missing heading")
	}
	if reactData[invoiceDetailPage](t, act).Seller != "U" {
		t.Error("act page did not use the same issuer as the invoice")
	}
	pdf := e.do("GET", loc+"/act.pdf", nil, nil)
	b := readBody(t, pdf)
	if pdf.StatusCode != 200 || !strings.HasPrefix(b, "%PDF") {
		t.Errorf("act pdf: %d", pdf.StatusCode)
	}
}
