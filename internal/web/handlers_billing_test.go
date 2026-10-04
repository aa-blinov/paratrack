package web

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

type invoiceDetailsStub struct {
	InvoiceQueries
	details model.InvoiceDetails
}

func (s invoiceDetailsStub) Get(context.Context, int64, int64) (model.InvoiceDetails, error) {
	return s.details, nil
}

type billingRulesErrorStub struct {
	TeamSettingsManagement
	err error
}

func (s billingRulesErrorStub) BillingRules(context.Context, int64) (model.BillingRules, error) {
	return model.BillingRules{}, s.err
}

func TestLoadInvoiceVMReturnsBillingRulesError(t *testing.T) {
	wantErr := errors.New("billing rules unavailable")
	server := &Server{services: Dependencies{
		Invoicing: InvoiceDependencies{Queries: invoiceDetailsStub{details: model.InvoiceDetails{Invoice: model.Invoice{
			ID: 4, TeamID: 8, Currency: "RUB",
			PeriodStart: time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC),
			PeriodEnd:   time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC),
			CreatedAt:   time.Date(2026, time.October, 2, 0, 0, 0, 0, time.UTC),
		}}}},
		Teams: TeamDependencies{Settings: billingRulesErrorStub{err: wantErr}},
	}}
	request := httptest.NewRequest("GET", "/invoices/4", nil)
	if _, _, err := server.loadInvoiceVM(request); !errors.Is(err, wantErr) {
		t.Fatalf("loadInvoiceVM() error = %v, want wrapped billing rules error", err)
	}
}
