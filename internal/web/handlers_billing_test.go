package web

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

type invoiceDetailsStub struct {
	InvoiceQueries
	details appmodel.InvoiceDetailResult
}

func (s invoiceDetailsStub) Get(context.Context, int64, int64) (appmodel.InvoiceDetailResult, error) {
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
		Invoicing: InvoiceDependencies{Queries: invoiceDetailsStub{details: appmodel.InvoiceDetailResult{Invoice: model.Invoice{
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

type sellerIdentityStub struct {
	IdentityWorkflow
	user appmodel.UserIdentity
	err  error
}

func (s sellerIdentityStub) IdentityByID(context.Context, int64) (appmodel.UserIdentity, error) {
	return s.user, s.err
}

func TestSellerNameUsesOwnerNameForPersonalWorkspace(t *testing.T) {
	server := &Server{services: Dependencies{
		Auth: AuthenticationDependencies{Identity: sellerIdentityStub{user: appmodel.UserIdentity{ID: 7, Name: "Owner"}}},
	}}
	team := model.Team{ID: 9, OwnerID: 7, Slug: "personal-7-owner", Name: "Owner's workspace"}
	request := httptest.NewRequest("GET", "/invoices/4", nil).WithContext(WithTeam(context.Background(), team))
	if got := server.sellerName(request); got != "Owner" {
		t.Fatalf("sellerName() = %q, want owner name", got)
	}
}

func TestSellerNameLogsLookupFailureAndFallsBackToTeamName(t *testing.T) {
	var logs bytes.Buffer
	wantErr := errors.New("identity store unavailable")
	server := &Server{
		logger: log.New(&logs, "", 0),
		services: Dependencies{
			Auth: AuthenticationDependencies{Identity: sellerIdentityStub{err: wantErr}},
		},
	}
	team := model.Team{ID: 9, OwnerID: 7, Slug: "personal-7-owner", Name: "Owner's workspace"}
	request := httptest.NewRequest("GET", "/invoices/4", nil).WithContext(WithTeam(context.Background(), team))
	if got := server.sellerName(request); got != team.Name {
		t.Fatalf("sellerName() = %q, want team name fallback", got)
	}
	if got := logs.String(); !strings.Contains(got, "invoice issuer name for user 7") || !strings.Contains(got, wantErr.Error()) {
		t.Fatalf("issuer lookup log = %q", got)
	}
}
