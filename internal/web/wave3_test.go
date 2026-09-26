package web

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/db"
)



func TestBillableRateAndInvoiceFlow(t *testing.T) {
	d, err := newTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	d.SQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`)
	d.SQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)

	p, err := d.CreateProject(ctx, 1, "Client A", "", "#7c3aed")
	if err != nil {
		t.Fatal(err)
	}
	// 10000 cents/hour = 100.00
	rate := 10000
	if err := d.SetProjectRate(ctx, 1, p.ID, &rate, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := d.GetProject(ctx, p.ID)
	if got.BillableRateCents == nil || *got.BillableRateCents != 10000 {
		t.Fatalf("rate=%v", got.BillableRateCents)
	}

	// 2h of work in the project
	act, err := d.CreateActivity(ctx, 1, "consulting")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AssignActivityProject(ctx, 1, act.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	if _, err := d.CreateClosedSession(ctx, 1, act.ID, start, end, "billable work"); err != nil {
		t.Fatal(err)
	}

	// build lines: 2h × 100.00 = 200.00
	lines, err := d.BuildInvoiceLines(ctx, 1, start.Add(-time.Hour), end.Add(time.Hour), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("lines=%+v", lines)
	}
	if lines[0].AmountCents != 20000 {
		t.Errorf("amount=%d, want 20000", lines[0].AmountCents)
	}
	if lines[0].Seconds != 7200 {
		t.Errorf("seconds=%d, want 7200", lines[0].Seconds)
	}

	// non-billable project is excluded
	bFalse := false
	if err := d.SetProjectRate(ctx, 1, p.ID, nil, &bFalse); err != nil {
		t.Fatal(err)
	}
	lines, _ = d.BuildInvoiceLines(ctx, 1, start.Add(-time.Hour), end.Add(time.Hour), 0)
	if len(lines) != 0 {
		t.Fatalf("non-billable leaked: %+v", lines)
	}

	// invoice create + number
	bTrue := true
	if err := d.SetProjectRate(ctx, 1, p.ID, nil, &bTrue); err != nil {
		t.Fatal(err)
	}
	num, err := d.NextInvoiceNumber(ctx, 1)
	if err != nil || num == "" {
		t.Fatalf("number=%q err=%v", num, err)
	}
	inv, err := d.CreateInvoice(ctx, 1, num, "Acme", start, end, "net 14", []db.InvoiceLine{{
		Label: "Client A · consulting", Seconds: 7200, RateCents: 10000, AmountCents: 20000,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Number != num || inv.ClientName != "Acme" {
		t.Fatalf("inv=%+v", inv)
	}
	ls, _ := d.ListInvoiceLines(ctx, inv.ID)
	if len(ls) != 1 || ls[0].AmountCents != 20000 {
		t.Fatalf("lines=%+v", ls)
	}
	if err := d.UpdateInvoiceStatus(ctx, 1, inv.ID, "paid"); err != nil {
		t.Fatal(err)
	}
	got2, _ := d.GetInvoice(ctx, 1, inv.ID)
	if got2.Status != "paid" {
		t.Fatalf("status=%s", got2.Status)
	}
	// second number increments
	num2, _ := d.NextInvoiceNumber(ctx, 1)
	if num2 <= num {
		t.Errorf("number sequence: %s → %s", num, num2)
	}
}

func TestFormatMoney(t *testing.T) {
	cases := map[int]string{
		0:     "0.00",
		5:     "0.05",
		100:   "1.00",
		12345: "123.45",
		-50:   "-0.50",
	}
	for cents, want := range cases {
		if got := formatMoney(cents); got != want {
			t.Errorf("formatMoney(%d)=%q want %q", cents, got, want)
		}
	}
}
