package web

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// FIN-07: a sent invoice still unpaid past its due day is overdue. The day
// itself is not yet late, and a draft or a paid invoice never is.
func TestInvoiceOverdueNeedsSentUnpaidAndPastDue(t *testing.T) {
	cases := []struct {
		status, due string
		want        bool
	}{
		{"sent", "2026-10-21", true},
		{"sent", "2026-10-22", false},
		{"sent", "", false},
		{"draft", "2026-10-01", false},
		{"paid", "2026-10-01", false},
	}
	for _, c := range cases {
		if got := invoiceOverdue(c.status, c.due, "2026-10-22"); got != c.want {
			t.Errorf("invoiceOverdue(%q, %q) = %v, want %v", c.status, c.due, got, c.want)
		}
	}
}

// The due day is stored with the draft, read back on every invoice read, and
// cleared by an empty value.
func TestInvoiceDueDateIsStoredAndCleared(t *testing.T) {
	d, err := newTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	d.TestSQL().ExecContext(ctx, `INSERT INTO users (id, email, password_hash, name) VALUES (1,'a@x.t','x','A')`)
	d.TestSQL().ExecContext(ctx, `INSERT INTO teams (id, name, slug, owner_id) VALUES (1,'T','t',1)`)
	d.TestSQL().ExecContext(ctx, `INSERT INTO memberships (team_id, user_id, role) VALUES (1,1,'owner')`)
	ctx = requestctx.WithActor(ctx, 1)
	start := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	num, _ := d.NextInvoiceNumber(ctx, 1)
	inv, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{TeamID: 1, CallerID: 1, Number: num, Client: "Acme Corp", Start: start, End: end, Lines: []db.InvoiceLine{{
		Label: "Acme · consulting", Seconds: 7200, RateCents: 10000, AmountCents: 20000,
	}}})
	if err != nil {
		t.Fatal(err)
	}

	update := func(due string) {
		t.Helper()
		if err := d.UpdateInvoiceMetaAndProjectClient(ctx, appmodel.InvoiceDraftUpdateRequest{
			TeamID: 1, InvoiceID: inv.ID, CallerID: 1, Client: "Acme Corp", DueDate: due,
		}); err != nil {
			t.Fatal(err)
		}
	}
	dueOf := func() string {
		t.Helper()
		details, err := d.GetInvoiceDetails(ctx, appmodel.InvoiceLookupQuery{TeamID: 1, InvoiceID: inv.ID})
		if err != nil {
			t.Fatal(err)
		}
		list, err := d.ListInvoices(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 {
			t.Fatalf("list has %d invoices, want 1", len(list))
		}
		if list[0].DueDate != details.Invoice.DueDate {
			t.Fatalf("list due %q disagrees with details due %q", list[0].DueDate, details.Invoice.DueDate)
		}
		return details.Invoice.DueDate
	}

	if got := dueOf(); got != "" {
		t.Fatalf("a new invoice has due %q, want none", got)
	}
	update("2026-10-05")
	if got := dueOf(); got != "2026-10-05" {
		t.Fatalf("stored due = %q, want 2026-10-05", got)
	}
	update("")
	if got := dueOf(); got != "" {
		t.Fatalf("cleared due = %q, want none", got)
	}
}
