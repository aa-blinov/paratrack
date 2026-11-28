package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// The issue day belongs to the send, not to the look: a letter prepared for a
// draft states the day it was written, the column stays empty until the
// invoice actually goes out, and the register never reads the draft's
// creation day as the issue day.
func TestIssueDayComesFromTheSendNotTheLook(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "День выставления", "issue-day")
	ownerID := teamOwner(t, d, teamID)
	ctx = requestctx.WithActor(ctx, ownerID)
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	invoice, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{
		TeamID: teamID, CallerID: ownerID, Number: "INV-2026-777", Client: "Client",
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
	if err := d.SaveInvoiceIssuedSnapshot(ctx, appmodel.InvoiceSnapshotSaveRequest{
		TeamID: teamID, InvoiceID: invoice.ID, Data: `{"issued_on":"2026-06-10","labels":["Работа"]}`, IssuedOn: "2026-06-10",
	}); err != nil {
		t.Fatal(err)
	}
	if got := invoiceColumn(t, d, invoice.ID, "issued_on"); got != "" {
		t.Fatalf("a prepared look gave the draft the issue day %q", got)
	}
	if err := d.MarkInvoiceSentOnce(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: invoice.ID, CallerID: ownerID}); err != nil {
		t.Fatal(err)
	}
	if got := invoiceColumn(t, d, invoice.ID, "issued_on"); got != "2026-06-10" {
		t.Fatalf("issued_on = %q, want the day the letter states", got)
	}
	// A later letter for an already issued invoice never moves its day.
	if err := d.SaveInvoiceIssuedSnapshot(ctx, appmodel.InvoiceSnapshotSaveRequest{
		TeamID: teamID, InvoiceID: invoice.ID, Data: `{"issued_on":"2026-07-01"}`, IssuedOn: "2026-07-01",
	}); err != nil {
		t.Fatal(err)
	}
	if got := invoiceColumn(t, d, invoice.ID, "issued_on"); got != "2026-06-10" {
		t.Fatalf("issued_on after a second letter = %q, want 2026-06-10", got)
	}
}

// An invoice issued without a prepared look takes today in the workspace
// calendar: a sent document always has a day, and the day never stays empty.
func TestIssueDayWithoutALookIsToday(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	teamID := seedTeam(t, d, "Без письма", "issue-day-today")
	ownerID := teamOwner(t, d, teamID)
	ctx = requestctx.WithActor(ctx, ownerID)
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	invoice, err := d.CreateInvoice(ctx, appmodel.InvoiceCreateRequest{
		TeamID: teamID, CallerID: ownerID, Number: "INV-2026-778", Client: "Client",
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
	if err := d.MarkInvoiceSentOnce(ctx, appmodel.InvoiceMutationRequest{TeamID: teamID, InvoiceID: invoice.ID, CallerID: ownerID}); err != nil {
		t.Fatal(err)
	}
	got := invoiceColumn(t, d, invoice.ID, "issued_on")
	if _, err := time.Parse("2006-01-02", got); err != nil {
		t.Fatalf("issued_on = %q, want a plain day", got)
	}
}

func invoiceColumn(t *testing.T, d *DB, invoiceID int64, column string) string {
	t.Helper()
	var value string
	if err := d.TestSQL().QueryRowContext(t.Context(),
		`SELECT COALESCE(`+column+`, '') FROM invoices WHERE id = ?`, invoiceID).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
