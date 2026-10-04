package invoicedocuments

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

type invoiceReaderStub struct {
	details   appmodel.InvoiceDetailResult
	getErr    error
	stripe    bool
	stripeErr error
	teamID    int64
	invoiceID int64
}

func (stub *invoiceReaderStub) Get(_ context.Context, query appmodel.InvoiceLookupQuery) (appmodel.InvoiceDetailResult, error) {
	stub.teamID, stub.invoiceID = query.TeamID, query.InvoiceID
	return stub.details, stub.getErr
}

func (stub *invoiceReaderStub) StripeReady(context.Context, int64) (bool, error) {
	return stub.stripe, stub.stripeErr
}

type teamBillingReaderStub struct {
	rules  model.BillingRules
	teamID int64
	err    error
}

func (stub *teamBillingReaderStub) BillingRules(_ context.Context, teamID int64) (model.BillingRules, error) {
	stub.teamID = teamID
	return stub.rules, stub.err
}

type loggerStub struct{ messages []string }

func (stub *loggerStub) Printf(format string, args ...any) {
	stub.messages = append(stub.messages, fmt.Sprintf(format, args...))
}

func TestBuildAssemblesInvoiceAndBillingRules(t *testing.T) {
	details := appmodel.InvoiceDetailResult{Invoice: model.Invoice{ID: 12, TeamID: 4, Number: "INV-12"}, TotalCents: 900}
	invoices := &invoiceReaderStub{details: details, stripe: true}
	teams := &teamBillingReaderStub{rules: model.BillingRules{Logo: "data:image/png;base64,abc"}}
	builder, err := New(Dependencies{Invoices: invoices, Teams: teams, Logger: &loggerStub{}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := builder.Build(context.Background(), appmodel.InvoiceDocumentRequest{
		TeamID: 4, InvoiceID: 12, IncludeStripeReadiness: true,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if snapshot.Details.Invoice.ID != details.Invoice.ID || snapshot.Details.TotalCents != details.TotalCents || snapshot.BillingRules.Logo != teams.rules.Logo || !snapshot.StripeReady {
		t.Fatalf("invoice document snapshot = %+v", snapshot)
	}
	if invoices.teamID != 4 || invoices.invoiceID != 12 || teams.teamID != 4 {
		t.Fatalf("workflow reads used unexpected scope: invoice=%+v team=%+v", invoices, teams)
	}
}

func TestBuildKeepsDocumentAvailableWhenStripeReadinessFails(t *testing.T) {
	logger := &loggerStub{}
	wantErr := errors.New("Stripe settings unavailable")
	builder, err := New(Dependencies{
		Invoices: &invoiceReaderStub{details: appmodel.InvoiceDetailResult{Invoice: model.Invoice{ID: 12}}, stripeErr: wantErr},
		Teams:    &teamBillingReaderStub{rules: model.BillingRules{}}, Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := builder.Build(context.Background(), appmodel.InvoiceDocumentRequest{
		TeamID: 4, InvoiceID: 12, IncludeStripeReadiness: true,
	})
	if err != nil {
		t.Fatalf("optional Stripe readiness failure broke document read: %v", err)
	}
	if snapshot.StripeReady || len(logger.messages) != 1 {
		t.Fatalf("Stripe readiness fallback = %+v, logs=%v", snapshot, logger.messages)
	}
}
