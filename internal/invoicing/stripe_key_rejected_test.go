package invoicing

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// A key the gateway refuses reaches the caller as the same error, so the page can
// name the setting to fix. The workflow must not replace the reason with a generic one.
func TestRefusedKeyReachesTheCallerAsKeyRejected(t *testing.T) {
	writer := &draftWriterStub{}
	gateway := &stripeGatewayStub{checkoutErr: fmt.Errorf("%w: stripe returned status 401", appmodel.ErrStripeKeyRejected)}
	reader := &draftReaderStub{details: appmodel.InvoiceDetails{
		Invoice: model.Invoice{ID: 12, TeamID: 4, Number: "INV-12", Currency: "USD", Revision: 3},
		Lines:   []model.InvoiceLine{{AmountCents: 1000}},
	}}
	deps := testDependencies(reader, writer)
	deps.Authorizer = managerAuthorizerStub{role: model.TeamRoleOwner, member: true}
	deps.StripeCredentials, deps.StripeGateway = stripeCredentialsStub{key: "workspace-key"}, gateway
	service, err := NewService(deps)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CreateStripePaymentLink(context.Background(), appmodel.InvoiceStripeLinkRequest{TeamID: 4, InvoiceID: 12, CallerID: 9, SuccessURL: "https://app.example/invoices/12"})
	if !errors.Is(err, ErrStripeKeyRejected) {
		t.Fatalf("refused key = %v, want ErrStripeKeyRejected through the workflow", err)
	}
}
