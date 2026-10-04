package invoicing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

type draftReaderStub struct {
	Reader
	details       model.InvoiceDetails
	listDetails   []model.InvoiceDetails
	getCalls      int
	lookup        appmodel.InvoiceLookupQuery
	overlapLabels []string
	overlapErr    error
	unbilled      []model.UnbilledProject
	unbilledQuery appmodel.UnbilledProjectQuery
	unassigned    []model.UnassignedActivity
}

func (s *draftReaderStub) ListInvoiceDetails(context.Context, int64) ([]model.InvoiceDetails, error) {
	return s.listDetails, nil
}

func (s *draftReaderStub) Unbilled(_ context.Context, query appmodel.UnbilledProjectQuery) ([]model.UnbilledProject, error) {
	s.unbilledQuery = query
	return s.unbilled, nil
}

func (s *draftReaderStub) UnassignedActivities(context.Context, int64) ([]model.UnassignedActivity, error) {
	return s.unassigned, nil
}

func (s *draftReaderStub) GetInvoiceDetails(_ context.Context, query appmodel.InvoiceLookupQuery) (model.InvoiceDetails, error) {
	s.getCalls++
	s.lookup = query
	return s.details, nil
}

func (s *draftReaderStub) OverlappingInvoices(_ context.Context, _, _ int64, _, _ time.Time, labels []string) ([]string, error) {
	s.overlapLabels = append([]string(nil), labels...)
	return []string{"INV-OLD"}, s.overlapErr
}

type draftWriterStub struct {
	Writer
	invoice    model.Invoice
	lines      []model.InvoiceLine
	createErr  error
	assignment struct {
		team, activity, project, caller int64
		calls                           int
		err                             error
	}
	payment struct {
		teamID, invoiceID, callerID, revision int64
		url, sessionID                        string
		err                                   error
	}
	stripePayment struct {
		teamID, invoiceID int64
		sessionID         string
	}
}

type projectBillingStub struct {
	projects []model.Project
	clients  map[int64]model.ProjectClient
}

func (s projectBillingStub) ListProjects(context.Context, appmodel.ProjectCatalogQuery) ([]model.Project, error) {
	return s.projects, nil
}

func (s projectBillingStub) ListProjectClients(context.Context, int64) (map[int64]model.ProjectClient, error) {
	return s.clients, nil
}

type noopAuditRecorder struct{}

func (noopAuditRecorder) Record(context.Context, model.AuditRecord) error {
	return nil
}

type noopLogger struct{}

func (noopLogger) Printf(string, ...any) {}

type recordingLogger struct{ messages []string }

func (s *recordingLogger) Printf(format string, args ...any) {
	s.messages = append(s.messages, fmt.Sprintf(format, args...))
}

func testDependencies(reader Reader, writer Writer) Dependencies {
	return Dependencies{Reader: reader, Projects: projectBillingStub{}, Writer: writer, Audit: noopAuditRecorder{}, Logger: noopLogger{}}
}

func TestDraftOptionsAppliesBillingEligibilityAndClientDefaults(t *testing.T) {
	rate := 1250
	projects := projectBillingStub{
		projects: []model.Project{
			{ID: 1, Billable: true, BillableRateCents: &rate},
			{ID: 2, Billable: true, BillableRateCents: &rate, Archived: true},
			{ID: 3, Billable: true},
			{ID: 4, Billable: false, BillableRateCents: &rate},
		},
		clients: map[int64]model.ProjectClient{1: {Name: "Acme", Email: "billing@example.test"}},
	}
	deps := testDependencies(&draftReaderStub{}, &draftWriterStub{})
	deps.Projects = projects
	service, err := NewService(deps)
	if err != nil {
		t.Fatalf("construct invoicing service: %v", err)
	}
	options, err := service.DraftOptions(context.Background(), 7)
	if err != nil {
		t.Fatalf("DraftOptions: %v", err)
	}
	if !options.HasBillable || len(options.Projects) != 4 {
		t.Fatalf("DraftOptions = %+v", options)
	}
	if !options.Projects[0].Eligible || !options.Projects[0].HasClient || options.Projects[0].Client.Name != "Acme" {
		t.Fatalf("billable project option = %+v", options.Projects[0])
	}
	if options.Projects[1].Eligible || options.Projects[2].Eligible || options.Projects[3].Eligible {
		t.Fatalf("ineligible projects became selectable: %+v", options.Projects[1:])
	}
}

func TestBuildIndexAssemblesInvoiceListAndDraftHistory(t *testing.T) {
	reader := &draftReaderStub{
		listDetails: []model.InvoiceDetails{{Invoice: model.Invoice{ID: 14, TeamID: 7, Number: "INV-14"}}},
		unbilled:    []model.UnbilledProject{{ProjectID: 3, ProjectName: "Website"}},
		unassigned:  []model.UnassignedActivity{{ID: 9, Name: "Research", Sessions: 2}},
	}
	deps := testDependencies(reader, &draftWriterStub{})
	deps.Projects = projectBillingStub{projects: []model.Project{{ID: 3, Name: "Website", Billable: true, BillableRateCents: ptrInt(2000)}}}
	service, err := NewService(deps)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := service.BuildIndex(context.Background(), appmodel.InvoiceIndexRequest{TeamID: 7})
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	if len(snapshot.Invoices) != 1 || snapshot.Invoices[0].Invoice.ID != 14 {
		t.Fatalf("invoice list = %+v", snapshot.Invoices)
	}
	if !snapshot.DraftOptions.HasBillable || len(snapshot.DraftOptions.Projects) != 1 || !snapshot.DraftOptions.Projects[0].Eligible {
		t.Fatalf("draft options = %+v", snapshot.DraftOptions)
	}
	if len(snapshot.Unbilled) != 1 || snapshot.Unbilled[0].ProjectID != 3 || len(snapshot.Unassigned) != 1 || snapshot.Unassigned[0].ID != 9 {
		t.Fatalf("invoice history = unbilled %+v, unassigned %+v", snapshot.Unbilled, snapshot.Unassigned)
	}
	if want := (appmodel.UnbilledProjectQuery{TeamID: 7}); reader.unbilledQuery.TeamID != want.TeamID || reader.unbilledQuery.ProjectID != nil {
		t.Fatalf("invoice index unbilled query = %+v, want all projects in workspace %d", reader.unbilledQuery, want.TeamID)
	}
}

func ptrInt(value int) *int { return &value }

type recordingAudit struct{ calls [][6]any }

func (s *recordingAudit) Record(_ context.Context, record model.AuditRecord) error {
	s.calls = append(s.calls, [6]any{record.TeamID, record.UserID, record.Action, record.Target, record.Meta, record.IP})
	return nil
}

func (s *draftWriterStub) CreateInvoiceDraft(context.Context, appmodel.InvoiceDraftRequest) (model.Invoice, []model.InvoiceLine, error) {
	return s.invoice, s.lines, s.createErr
}

func (s *draftWriterStub) AssignUnassignedActivityForBilling(_ context.Context, request appmodel.AssignActivityProjectRequest) error {
	s.assignment.team, s.assignment.activity, s.assignment.project, s.assignment.caller = request.TeamID, request.ActivityID, request.ProjectID, request.CallerID
	s.assignment.calls++
	return s.assignment.err
}

func (s *draftWriterStub) SetPaymentURL(_ context.Context, request appmodel.InvoicePaymentLinkSaveRequest) error {
	s.payment.teamID, s.payment.invoiceID, s.payment.callerID, s.payment.revision = request.TeamID, request.InvoiceID, request.CallerID, request.ExpectedRevision
	s.payment.url, s.payment.sessionID = request.PaymentURL, request.StripeSessionID
	return s.payment.err
}

func (s *draftWriterStub) MarkInvoicePaidFromStripe(_ context.Context, request appmodel.InvoiceStripePaymentRequest) (bool, error) {
	s.stripePayment = struct {
		teamID, invoiceID int64
		sessionID         string
	}{request.TeamID, request.InvoiceID, request.StripeSessionID}
	return true, nil
}

type stripeCredentialsStub struct {
	key, secret string
	err         error
}

type managerAuthorizerStub struct {
	role   model.TeamRole
	member bool
	err    error
}

func (s managerAuthorizerStub) TeamMemberRole(context.Context, appmodel.TeamMembershipQuery) (model.TeamRole, bool, error) {
	return s.role, s.member, s.err
}

func (s stripeCredentialsStub) TeamStripe(context.Context, int64) (string, string, error) {
	return s.key, s.secret, s.err
}

type stripeGatewayStub struct {
	key      string
	request  StripeCheckoutRequest
	event    StripeWebhookEvent
	parseErr error
	header   string
	body     []byte
	secret   string
	now      time.Time
	valid    bool
	expire   struct {
		key, sessionID string
		ctxErr         error
		err            error
	}
}

func (s *stripeGatewayStub) ParseWebhookEvent([]byte) (StripeWebhookEvent, error) {
	return s.event, s.parseErr
}

func (s *stripeGatewayStub) CreateCheckout(_ context.Context, key string, request StripeCheckoutRequest) (string, string, error) {
	s.key, s.request = key, request
	return "cs_123", "https://checkout.example/session", nil
}

func (s *stripeGatewayStub) ExpireCheckout(ctx context.Context, key, sessionID string) error {
	s.expire.key, s.expire.sessionID, s.expire.ctxErr = key, sessionID, ctx.Err()
	return s.expire.err
}

func (s *stripeGatewayStub) VerifyWebhookSignature(header string, body []byte, secret string, now time.Time) bool {
	s.header, s.body, s.secret, s.now = header, append([]byte(nil), body...), secret, now
	return s.valid
}

func TestCreateStripePaymentLinkKeepsProviderCredentialsInWorkflow(t *testing.T) {
	writer := &draftWriterStub{}
	gateway := &stripeGatewayStub{}
	reader := &draftReaderStub{details: model.InvoiceDetails{
		Invoice: model.Invoice{ID: 12, TeamID: 4, Number: "INV-12", Currency: "USD", Revision: 3},
		Lines:   []model.InvoiceLine{{AmountCents: 1000}, {AmountCents: 1500}},
	}}
	audit := &recordingAudit{}
	deps := testDependencies(reader, writer)
	deps.Authorizer = managerAuthorizerStub{role: model.TeamRoleOwner, member: true}
	deps.StripeCredentials, deps.StripeGateway = stripeCredentialsStub{key: "workspace-key"}, gateway
	deps.Audit = audit
	service, err := NewService(deps)
	if err != nil {
		t.Fatal(err)
	}
	wantURL := "https://checkout.example/session"
	ctx := requestctx.WithClientIP(context.Background(), "203.0.113.2")
	got, err := service.CreateStripePaymentLink(ctx, appmodel.InvoiceStripeLinkRequest{TeamID: 4, InvoiceID: 12, CallerID: 9, SuccessURL: "https://app.example/invoices/12"})
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != wantURL || got.InvoiceNumber != "INV-12" || gateway.key != "workspace-key" {
		t.Fatalf("checkout result = %+v with key %q", got, gateway.key)
	}
	if gateway.request.TeamID != 4 || gateway.request.InvoiceID != 12 || gateway.request.TotalCents != 2500 || gateway.request.Currency != "USD" || gateway.request.InvoiceNumber != "INV-12" {
		t.Fatalf("checkout request = %+v", gateway.request)
	}
	if writer.payment.teamID != 4 || writer.payment.invoiceID != 12 || writer.payment.callerID != 9 || writer.payment.revision != 3 || writer.payment.url != wantURL || writer.payment.sessionID != "cs_123" {
		t.Fatalf("persisted payment link = %+v", writer.payment)
	}
	if len(audit.calls) != 1 || audit.calls[0] != [6]any{int64(4), int64(9), "invoice.paylink", "INV-12", "stripe", "203.0.113.2"} {
		t.Fatalf("payment-link audit = %v", audit.calls)
	}
}

func TestGetCalculatesInvoiceTotals(t *testing.T) {
	reader := &draftReaderStub{details: model.InvoiceDetails{
		Invoice: model.Invoice{ID: 12, TeamID: 4},
		Lines: []model.InvoiceLine{
			{Seconds: 1800, AmountCents: 1250},
			{Seconds: 5400, AmountCents: 3750},
		},
	}}
	service, err := NewService(testDependencies(reader, &draftWriterStub{}))
	if err != nil {
		t.Fatal(err)
	}

	got, err := service.Get(context.Background(), 4, 12)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.TotalCents != 5000 || got.TotalHoursHundredths != 200 {
		t.Fatalf("Get() totals = %d cents, %d hundredths; want 5000 and 200", got.TotalCents, got.TotalHoursHundredths)
	}
	if want := (appmodel.InvoiceLookupQuery{TeamID: 4, InvoiceID: 12}); reader.lookup != want {
		t.Fatalf("GetInvoiceDetails query = %+v, want %+v", reader.lookup, want)
	}
}

func TestListCalculatesInvoiceSummaryTotals(t *testing.T) {
	reader := &draftReaderStub{listDetails: []model.InvoiceDetails{{
		Invoice: model.Invoice{ID: 13, TeamID: 4, Number: "INV-13"},
		Lines: []model.InvoiceLine{
			{Seconds: 1800, AmountCents: 1250},
			{Seconds: 5400, AmountCents: 3750},
		},
	}}}
	service, err := NewService(testDependencies(reader, &draftWriterStub{}))
	if err != nil {
		t.Fatal(err)
	}

	got, err := service.List(context.Background(), 4)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 1 || got[0].Invoice.Number != "INV-13" || got[0].TotalCents != 5000 || got[0].TotalHoursHundredths != 200 {
		t.Fatalf("List() summaries = %+v", got)
	}
}

func TestGetRejectsInvoiceTotalOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	reader := &draftReaderStub{details: model.InvoiceDetails{
		Invoice: model.Invoice{ID: 12},
		Lines:   []model.InvoiceLine{{AmountCents: maxInt}, {AmountCents: 1}},
	}}
	service, err := NewService(testDependencies(reader, &draftWriterStub{}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(context.Background(), 4, 12); !errors.Is(err, money.ErrOverflow) {
		t.Fatalf("Get() overflow error = %v, want %v", err, money.ErrOverflow)
	}
}

func TestCreateStripePaymentLinkExpiresSessionWhenPersistenceFails(t *testing.T) {
	writeErr := errors.New("invoice changed during checkout")
	writer := &draftWriterStub{}
	writer.payment.err = writeErr
	gateway := &stripeGatewayStub{}
	gateway.expire.err = errors.New("Stripe unavailable")
	logger := &recordingLogger{}
	reader := &draftReaderStub{details: model.InvoiceDetails{
		Invoice: model.Invoice{ID: 12, TeamID: 4, Number: "INV-12", Currency: "USD", Revision: 3},
		Lines:   []model.InvoiceLine{{AmountCents: 2500}},
	}}
	deps := testDependencies(reader, writer)
	deps.Authorizer = managerAuthorizerStub{role: model.TeamRoleOwner, member: true}
	deps.StripeCredentials, deps.StripeGateway = stripeCredentialsStub{key: "workspace-key"}, gateway
	deps.Logger = logger
	service, err := NewService(deps)
	if err != nil {
		t.Fatalf("construct invoice service: %v", err)
	}
	requestCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = service.CreateStripePaymentLink(requestCtx, appmodel.InvoiceStripeLinkRequest{TeamID: 4, InvoiceID: 12, CallerID: 9, SuccessURL: "https://app.example/invoices/12"})
	if !errors.Is(err, writeErr) {
		t.Fatalf("create payment link error = %v, want original persistence error", err)
	}
	if gateway.expire.key != "workspace-key" || gateway.expire.sessionID != "cs_123" || gateway.expire.ctxErr != nil {
		t.Fatalf("checkout cleanup = %+v, want detached cleanup with original credentials and session", gateway.expire)
	}
	if len(logger.messages) != 1 || !strings.Contains(logger.messages[0], `team 4 invoice 12 session "cs_123"`) ||
		strings.Contains(logger.messages[0], "workspace-key") || strings.Contains(logger.messages[0], "checkout.example") {
		t.Fatalf("cleanup log = %v, want safe team/invoice/session identifiers without key or URL", logger.messages)
	}
}

func TestSetManualPaymentLinkRecordsAuditAfterWrite(t *testing.T) {
	writer := &draftWriterStub{}
	reader := &draftReaderStub{details: model.InvoiceDetails{Invoice: model.Invoice{Number: "INV-12", Revision: 5}}}
	audit := &recordingAudit{}
	deps := testDependencies(reader, writer)
	deps.Audit = audit
	service, err := NewService(deps)
	if err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithClientIP(context.Background(), "203.0.113.3")
	if err := service.SetManualPaymentLink(ctx, appmodel.InvoiceManualLinkRequest{TeamID: 4, InvoiceID: 12, CallerID: 9, PaymentURL: "https://pay.example/inv/12"}); err != nil {
		t.Fatal(err)
	}
	if writer.payment.url != "https://pay.example/inv/12" || writer.payment.revision != 5 || len(audit.calls) != 1 || audit.calls[0] != [6]any{int64(4), int64(9), "invoice.paylink", "INV-12", "manual", "203.0.113.3"} {
		t.Fatalf("payment=%+v audit=%v", writer.payment, audit.calls)
	}
}

func TestCreateStripePaymentLinkAuthorizesBeforeReadingInvoiceOrCallingProvider(t *testing.T) {
	reader := &draftReaderStub{}
	writer := &draftWriterStub{}
	gateway := &stripeGatewayStub{}
	deps := testDependencies(reader, writer)
	deps.Authorizer = managerAuthorizerStub{role: model.TeamRoleMember, member: true}
	deps.StripeCredentials, deps.StripeGateway = stripeCredentialsStub{key: "workspace-key"}, gateway
	service, err := NewService(deps)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CreateStripePaymentLink(context.Background(), appmodel.InvoiceStripeLinkRequest{TeamID: 4, InvoiceID: 12, CallerID: 9, SuccessURL: "https://app.example/invoices/12"})
	if !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("authorization error = %v, want %v", err, model.ErrForbidden)
	}
	if reader.getCalls != 0 || gateway.key != "" || writer.payment.sessionID != "" {
		t.Fatalf("unauthorized request reached invoice/provider: reads=%d key=%q payment=%+v", reader.getCalls, gateway.key, writer.payment)
	}
}

func TestVerifyStripeWebhookUsesWorkspaceSecretAndProcessFallback(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	gateway := &stripeGatewayStub{valid: true, event: StripeWebhookEvent{Type: "customer.created", TeamID: "4"}}
	deps := testDependencies(&draftReaderStub{}, &draftWriterStub{})
	deps.StripeCredentials, deps.StripeGateway = stripeCredentialsStub{secret: "workspace-secret"}, gateway
	deps.StripeWebhookSecret = "process-secret"
	service, err := NewService(deps)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"type":"checkout.session.completed"}`)
	result, err := service.ProcessStripeWebhook(context.Background(), appmodel.StripePaymentEvent{Signature: "signature", Body: body, ReceivedAt: now})
	if err != nil || result.Changed {
		t.Fatalf("workspace webhook result = %+v, %v", result, err)
	}
	if gateway.secret != "workspace-secret" || gateway.header != "signature" || string(gateway.body) != string(body) || !gateway.now.Equal(now) {
		t.Fatalf("verification input = secret %q, header %q, body %q, time %v", gateway.secret, gateway.header, gateway.body, gateway.now)
	}

	gateway.event.TeamID = ""
	result, err = service.ProcessStripeWebhook(context.Background(), appmodel.StripePaymentEvent{Signature: "fallback-signature", Body: body, ReceivedAt: now})
	if err != nil || result.Changed || gateway.secret != "process-secret" {
		t.Fatalf("fallback verification result = %+v, %v with secret %q", result, err, gateway.secret)
	}
}

func TestProcessStripeWebhookRecordsOnlyVerifiedPaidEvents(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	writer := &draftWriterStub{}
	gateway := &stripeGatewayStub{
		valid: true,
		event: StripeWebhookEvent{Type: "checkout.session.completed", SessionID: "cs_123", InvoiceID: "12", TeamID: "4", PaymentStatus: "paid"},
	}
	deps := testDependencies(&draftReaderStub{}, writer)
	deps.StripeCredentials, deps.StripeGateway = stripeCredentialsStub{secret: "workspace-secret"}, gateway
	service, err := NewService(deps)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ProcessStripeWebhook(context.Background(), appmodel.StripePaymentEvent{Signature: "signature", Body: []byte("payload"), ReceivedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if result.TeamID != 4 || result.InvoiceID != 12 || !result.Changed {
		t.Fatalf("webhook result = %+v", result)
	}
	if writer.stripePayment.teamID != 4 || writer.stripePayment.invoiceID != 12 || writer.stripePayment.sessionID != "cs_123" {
		t.Fatalf("recorded Stripe payment = %+v", writer.stripePayment)
	}

	gateway.valid = false
	if _, err := service.ProcessStripeWebhook(context.Background(), appmodel.StripePaymentEvent{Signature: "bad-signature", Body: []byte("payload"), ReceivedAt: now}); !errors.Is(err, ErrInvalidStripeSignature) {
		t.Fatalf("invalid signature error = %v", err)
	}
}

func TestStripePayloadErrorPreservesDomainAndProviderCauses(t *testing.T) {
	providerErr := errors.New("malformed provider event")
	gateway := &stripeGatewayStub{parseErr: providerErr}
	deps := testDependencies(&draftReaderStub{}, &draftWriterStub{})
	deps.StripeGateway = gateway
	service, err := NewService(deps)
	if err != nil {
		t.Fatalf("construct service: %v", err)
	}
	_, err = service.ProcessStripeWebhook(context.Background(), appmodel.StripePaymentEvent{Signature: "signature", Body: []byte("payload")})
	if !errors.Is(err, ErrInvalidStripePayload) || !errors.Is(err, providerErr) {
		t.Fatalf("webhook error = %v, want both payload and provider causes", err)
	}
}

func TestCreateDraftWithOverlapCheckKeepsAdvisoryFailureNonBlocking(t *testing.T) {
	advisoryErr := errors.New("overlap query unavailable")
	reader := &draftReaderStub{overlapErr: advisoryErr}
	writer := &draftWriterStub{
		invoice: model.Invoice{ID: 12, Number: "INV-12"},
		lines:   []model.InvoiceLine{{Label: "Design"}, {Label: "Engineering"}},
	}
	service, err := NewService(testDependencies(reader, writer))
	if err != nil {
		t.Fatalf("construct service: %v", err)
	}
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	request := appmodel.InvoiceDraftRequest{TeamID: 3, CallerID: 4, Client: "Client", Start: start, End: start.AddDate(0, 0, 1), Options: appmodel.InvoiceOptions{ProjectID: 8}}
	created, err := service.CreateDraftWithOverlapCheck(context.Background(), request)
	if err != nil {
		t.Fatalf("draft creation should succeed despite advisory failure: %v", err)
	}
	if created.Invoice.ID != 12 || !errors.Is(created.AdvisoryError, advisoryErr) {
		t.Fatalf("unexpected creation result: invoice=%d advisory=%v", created.Invoice.ID, created.AdvisoryError)
	}
	if len(created.Overlaps) != 0 {
		t.Fatalf("overlaps = %v, want no results when the advisory query fails", created.Overlaps)
	}
	if len(reader.overlapLabels) != 2 || reader.overlapLabels[0] != "Design" || reader.overlapLabels[1] != "Engineering" {
		t.Fatalf("overlap labels = %v", reader.overlapLabels)
	}
}

func TestCreateDraftPublishesAuditAndEventOnlyAfterWrite(t *testing.T) {
	reader := &draftReaderStub{}
	writer := &draftWriterStub{invoice: model.Invoice{ID: 12, Number: "INV-12", ClientName: "Client"}}
	audit := &recordingAudit{}
	deps := testDependencies(reader, writer)
	deps.Audit = audit
	service, err := NewService(deps)
	if err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithClientIP(context.Background(), "203.0.113.9")
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	request := appmodel.InvoiceDraftRequest{TeamID: 3, CallerID: 4, Client: "Client", Start: start, End: start.AddDate(0, 0, 1), Options: appmodel.InvoiceOptions{ProjectID: 8}}
	if _, err := service.CreateDraftWithOverlapCheck(ctx, request); err != nil {
		t.Fatal(err)
	}
	wantAudit := [6]any{int64(3), int64(4), "invoice.create", "INV-12", "Client", "203.0.113.9"}
	if len(audit.calls) != 1 || audit.calls[0] != wantAudit {
		t.Fatalf("audit calls = %v", audit.calls)
	}
	writer.createErr = errors.New("write failed")
	if _, err := service.CreateDraftWithOverlapCheck(ctx, request); err == nil {
		t.Fatal("failed invoice write was ignored")
	}
	if len(audit.calls) != 1 {
		t.Fatalf("failed write emitted audit effects: %d", len(audit.calls))
	}
}

func TestAssignHistoryAuditsOnlyConfirmedSuccessfulWrite(t *testing.T) {
	writer := &draftWriterStub{}
	audit := &recordingAudit{}
	deps := testDependencies(&draftReaderStub{}, writer)
	deps.Audit = audit
	service, err := NewService(deps)
	if err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithClientIP(context.Background(), "203.0.113.13")
	if err := service.AssignHistoryToBillableProject(ctx, appmodel.AssignBillableHistoryRequest{TeamID: 3, ActivityID: 10, ProjectID: 20, CallerID: 4}); !errors.Is(err, ErrHistoryConfirmationRequired) {
		t.Fatalf("unconfirmed assignment error = %v", err)
	}
	if writer.assignment.calls != 0 || len(audit.calls) != 0 {
		t.Fatalf("unconfirmed assignment wrote state=%+v audit=%+v", writer.assignment, audit.calls)
	}
	if err := service.AssignHistoryToBillableProject(ctx, appmodel.AssignBillableHistoryRequest{TeamID: 3, ActivityID: 10, ProjectID: 20, CallerID: 4, Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	want := [6]any{int64(3), int64(4), "activity.project", "10", "20", "203.0.113.13"}
	if writer.assignment.calls != 1 || writer.assignment.team != 3 || writer.assignment.activity != 10 || writer.assignment.project != 20 || writer.assignment.caller != 4 || len(audit.calls) != 1 || audit.calls[0] != want {
		t.Fatalf("assignment=%+v audit=%v", writer.assignment, audit.calls)
	}
	writer.assignment.err = model.ErrForbidden
	if err := service.AssignHistoryToBillableProject(ctx, appmodel.AssignBillableHistoryRequest{TeamID: 3, ActivityID: 10, ProjectID: 20, CallerID: 4, Confirmed: true}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("failed assignment error = %v", err)
	}
	if len(audit.calls) != 1 {
		t.Fatalf("failed assignment was audited: %v", audit.calls)
	}
}
