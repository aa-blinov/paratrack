// Package invoicedocuments assembles the workflow data shared by invoice pages
// and generated documents.
package invoicedocuments

import (
	"context"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
)

type InvoiceReader interface {
	Get(context.Context, int64, int64) (appmodel.InvoiceDetailResult, error)
	StripeReady(context.Context, int64) (bool, error)
}

type TeamBillingReader interface {
	BillingRules(context.Context, int64) (model.BillingRules, error)
}

type Logger interface {
	Printf(string, ...any)
}

type Dependencies struct {
	Invoices InvoiceReader
	Teams    TeamBillingReader
	Logger   Logger
}

var ErrIncompleteDependencies = errors.New("invoice document builder dependencies are incomplete")

type Builder struct {
	invoices InvoiceReader
	teams    TeamBillingReader
	logger   Logger
}

func New(deps Dependencies) (*Builder, error) {
	for _, dependency := range []struct {
		name string
		port any
	}{
		{"invoice reader", deps.Invoices},
		{"team billing reader", deps.Teams},
		{"logger", deps.Logger},
	} {
		if depcheck.IsNil(dependency.port) {
			return nil, fmt.Errorf("%w: %s", ErrIncompleteDependencies, dependency.name)
		}
	}
	return &Builder{invoices: deps.Invoices, teams: deps.Teams, logger: deps.Logger}, nil
}

func (b *Builder) Build(ctx context.Context, request appmodel.InvoiceDocumentRequest) (appmodel.InvoiceDocumentSnapshot, error) {
	if request.TeamID <= 0 || request.InvoiceID <= 0 {
		return appmodel.InvoiceDocumentSnapshot{}, model.ErrNotFound
	}
	details, err := b.invoices.Get(ctx, request.TeamID, request.InvoiceID)
	if err != nil {
		return appmodel.InvoiceDocumentSnapshot{}, fmt.Errorf("load invoice %d details: %w", request.InvoiceID, err)
	}
	rules, err := b.teams.BillingRules(ctx, request.TeamID)
	if err != nil {
		return appmodel.InvoiceDocumentSnapshot{}, fmt.Errorf("load billing rules for invoice %d: %w", request.InvoiceID, err)
	}
	snapshot := appmodel.InvoiceDocumentSnapshot{Details: details, BillingRules: rules}
	if request.IncludeStripeReadiness {
		ready, err := b.invoices.StripeReady(ctx, request.TeamID)
		if err != nil {
			b.logger.Printf("invoicedocuments: load Stripe readiness for invoice %d: %v", request.InvoiceID, err)
		} else {
			snapshot.StripeReady = ready
		}
	}
	return snapshot, nil
}
