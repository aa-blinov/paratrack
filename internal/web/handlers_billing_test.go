package web

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

type invoiceDocumentBuilderStub struct{ err error }

func (stub invoiceDocumentBuilderStub) Build(context.Context, appmodel.InvoiceDocumentRequest) (appmodel.InvoiceDocumentSnapshot, error) {
	return appmodel.InvoiceDocumentSnapshot{}, stub.err
}

func TestLoadInvoiceVMReturnsInvoiceDocumentWorkflowError(t *testing.T) {
	wantErr := errors.New("billing rules unavailable")
	server := &Server{services: Dependencies{InvoiceDocuments: invoiceDocumentBuilderStub{err: wantErr}}}
	request := httptest.NewRequest("GET", "/invoices/4", nil)
	if _, _, _, err := server.loadInvoiceVM(request, false); !errors.Is(err, wantErr) {
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
