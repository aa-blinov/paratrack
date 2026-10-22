package invoicing

import (
	"context"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// The accountant's export lists the lines of the issued invoices only, for one
// client and a period of invoice dates, with the line's own hours, rate and amount
// (FIN-05). A draft stays out.
func TestExportLinesKeepsIssuedInvoicesForOneClientAndPeriod(t *testing.T) {
	october := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	reader := &draftReaderStub{listDetails: []appmodel.InvoiceDetails{
		{
			Invoice: model.Invoice{ID: 1, TeamID: 7, Number: "INV-1", ClientName: "Ромашка", Status: "sent", Currency: "RUB", CreatedAt: october},
			Lines:   []model.InvoiceLine{{Label: "Сайт · Вёрстка", Seconds: 5400, RateCents: 300000, AmountCents: 450000}},
		},
		{
			Invoice: model.Invoice{ID: 2, TeamID: 7, Number: "INV-2", ClientName: "Ромашка", Status: "draft", Currency: "RUB", CreatedAt: october},
			Lines:   []model.InvoiceLine{{Label: "Сайт · Макет", Seconds: 3600, RateCents: 300000, AmountCents: 300000}},
		},
		{
			Invoice: model.Invoice{ID: 3, TeamID: 7, Number: "INV-3", ClientName: "Лютик", Status: "paid", Currency: "RUB", CreatedAt: october},
			Lines:   []model.InvoiceLine{{Label: "Блог", Seconds: 1800, RateCents: 200000, AmountCents: 100000, Currency: "USD"}},
		},
		{
			Invoice: model.Invoice{ID: 4, TeamID: 7, Number: "INV-4", ClientName: "Ромашка", Status: "sent", Currency: "RUB", CreatedAt: october.AddDate(0, -1, 0)},
			Lines:   []model.InvoiceLine{{Label: "Старое", Seconds: 3600, RateCents: 300000, AmountCents: 300000}},
		},
	}}
	service, err := NewService(testDependencies(reader, &draftWriterStub{}))
	if err != nil {
		t.Fatal(err)
	}

	rows, err := service.ExportLines(context.Background(), appmodel.InvoiceExportQuery{
		TeamID: 7, Client: "Ромашка", From: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].InvoiceNumber != "INV-1" || rows[0].Seconds != 5400 || rows[0].AmountCents != 450000 {
		t.Fatalf("the export holds %+v, want the one issued line of the client in October", rows)
	}
	if rows[0].Currency != "RUB" {
		t.Fatalf("a line without its own currency takes the invoice's: got %q", rows[0].Currency)
	}

	all, err := service.ExportLines(context.Background(), appmodel.InvoiceExportQuery{TeamID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("the unfiltered export holds %d lines, want the three issued ones", len(all))
	}
	if all[1].Currency != "USD" {
		t.Fatalf("a line with its own currency kept %q, want USD", all[1].Currency)
	}
}
