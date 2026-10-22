package invoicing

import (
	"context"
	"errors"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// dueWriterStub records the draft update the service hands to the store.
type dueWriterStub struct {
	Writer
	update appmodel.InvoiceDraftUpdateRequest
}

func (s *dueWriterStub) UpdateInvoiceMetaAndProjectClient(_ context.Context, request appmodel.InvoiceDraftUpdateRequest) error {
	s.update = request
	return nil
}

// The payment due day is optional and must be a real date (FIN-07). An empty
// day is kept as "no due day"; a malformed one is refused before anything is
// written.
func TestDraftDueDateIsOptionalAndMustBeADate(t *testing.T) {
	reader := &draftReaderStub{details: appmodel.InvoiceDetails{Invoice: model.Invoice{ID: 9, Number: "INV-1", Status: "draft"}}}
	writer := &dueWriterStub{}
	service, err := NewService(testDependencies(reader, writer))
	if err != nil {
		t.Fatal(err)
	}
	request := appmodel.InvoiceDraftUpdateRequest{TeamID: 7, InvoiceID: 9, CallerID: 3, Client: "Acme"}

	for _, due := range []string{"", "2026-10-05"} {
		request.DueDate = due
		if err := service.UpdateDraftDetails(t.Context(), request); err != nil {
			t.Fatalf("due %q refused: %v", due, err)
		}
		if writer.update.DueDate != due {
			t.Fatalf("stored due = %q, want %q", writer.update.DueDate, due)
		}
	}
	for _, due := range []string{"05.10.2026", "2026-13-40", "soon"} {
		request.DueDate = due
		if err := service.UpdateDraftDetails(t.Context(), request); !errors.Is(err, ErrInvalidDue) {
			t.Fatalf("due %q: err = %v, want ErrInvalidDue", due, err)
		}
	}
}
