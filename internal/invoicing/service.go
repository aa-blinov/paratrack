// Package invoicing owns invoice creation workflows shared by HTTP and
// future non-HTTP adapters.
package invoicing

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
	"github.com/aa-blinov/paratrack/internal/postcommit"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// Reader exposes team-scoped invoice and billable-time queries.
type Reader interface {
	ListInvoiceDetails(context.Context, int64) ([]model.InvoiceDetails, error)
	GetInvoiceDetails(context.Context, appmodel.InvoiceLookupQuery) (model.InvoiceDetails, error)
	Unbilled(context.Context, int64, int64) ([]model.UnbilledProject, error)
	UnassignedActivities(context.Context, int64) ([]model.UnassignedActivity, error)
	OverlappingInvoices(context.Context, int64, int64, time.Time, time.Time, []string) ([]string, error)
}

// ProjectBillingReader provides the project catalog data needed to prepare
// invoice draft choices without making the transport coordinate project reads.
type ProjectBillingReader interface {
	ListProjects(context.Context, appmodel.ProjectCatalogQuery) ([]model.Project, error)
	ListProjectClients(context.Context, int64) (map[int64]model.ProjectClient, error)
}

// Writer exposes invoice transitions. The persistence adapter owns locking
// and atomic writes for these operations.
type Writer interface {
	CreateInvoiceDraft(context.Context, appmodel.InvoiceDraftRequest) (model.Invoice, []model.InvoiceLine, error)
	MarkInvoiceSentOnce(context.Context, appmodel.InvoiceMutationRequest) error
	MarkInvoicePaidOnce(context.Context, appmodel.InvoiceMutationRequest) (string, bool, error)
	MarkInvoicePaidFromStripe(context.Context, appmodel.InvoiceStripePaymentRequest) (bool, error)
	RebuildInvoice(context.Context, appmodel.InvoiceMutationRequest) error
	DeleteInvoice(context.Context, appmodel.InvoiceMutationRequest) error
	UpdateInvoiceMetaAndProjectClient(context.Context, appmodel.InvoiceDraftUpdateRequest) error
	SetPaymentURL(context.Context, appmodel.InvoicePaymentLinkSaveRequest) error
	AssignUnassignedActivityForBilling(context.Context, appmodel.AssignActivityProjectRequest) error
	SetInvoiceReceipt(context.Context, appmodel.InvoiceReceiptRequest) error
}

// StripeCredentials reads workspace credentials without exposing them to
// transport adapters.
type StripeCredentials interface {
	TeamStripe(context.Context, int64) (string, string, error)
}

// ManagerAuthorizer rechecks the actor's current workspace role before
// creating an external payment session.
type ManagerAuthorizer interface {
	TeamMemberRole(context.Context, appmodel.TeamMembershipQuery) (model.TeamRole, bool, error)
}

// StripeGateway contains the provider protocol operations used by invoicing.
// Its implementation belongs to the outbound adapter layer.
type StripeGateway interface {
	CreateCheckout(context.Context, string, StripeCheckoutRequest) (string, string, error)
	ExpireCheckout(context.Context, string, string) error
	ParseWebhookEvent([]byte) (StripeWebhookEvent, error)
	VerifyWebhookSignature(string, []byte, string, time.Time) bool
}

// StripeWebhookEvent is the provider payload subset needed by the invoicing
// workflow after protocol decoding.
type StripeWebhookEvent struct {
	Type          string
	SessionID     string
	InvoiceID     string
	TeamID        string
	PaymentStatus string
}

// StripeWebhookResult preserves the invoice workflow's public result name.
type StripeWebhookResult = appmodel.StripeWebhookResult

type StripeCheckoutRequest struct {
	InvoiceID     int64
	TeamID        int64
	TotalCents    int
	Currency      string
	InvoiceNumber string
	SuccessURL    string
}

type StripePaymentLink = appmodel.InvoiceStripePaymentLink

// Dependencies separates invoice queries from transactional commands.
type Dependencies struct {
	Reader              Reader
	Projects            ProjectBillingReader
	Writer              Writer
	Authorizer          ManagerAuthorizer
	StripeCredentials   StripeCredentials
	StripeGateway       StripeGateway
	StripeAPIKey        string
	StripeWebhookSecret string
	Audit               AuditRecorder
	Logger              Logger
}

type AuditRecorder interface {
	Record(context.Context, model.AuditRecord) error
}

type Logger interface {
	Printf(string, ...any)
}

var ErrIncompleteDependencies = errors.New("invoicing service dependencies are incomplete")

// Service coordinates invoice draft creation policy.
type Service struct {
	reader              Reader
	projects            ProjectBillingReader
	writer              Writer
	authorizer          ManagerAuthorizer
	stripeCredentials   StripeCredentials
	stripeGateway       StripeGateway
	stripeAPIKey        string
	stripeWebhookSecret string
	audit               AuditRecorder
	logger              Logger
}

// DraftCreation is the result of creating a draft and running its advisory
// duplicate-document check. A failed advisory check never rolls back the draft.
type DraftCreation = appmodel.InvoiceDraftCreation

func NewService(deps Dependencies) (*Service, error) {
	if depcheck.IsNil(deps.Reader) {
		return nil, fmt.Errorf("%w: invoice reader", ErrIncompleteDependencies)
	}
	if depcheck.IsNil(deps.Writer) {
		return nil, fmt.Errorf("%w: invoice writer", ErrIncompleteDependencies)
	}
	if depcheck.IsNil(deps.Projects) {
		return nil, fmt.Errorf("%w: project billing reader", ErrIncompleteDependencies)
	}
	if depcheck.IsNil(deps.Audit) {
		return nil, fmt.Errorf("%w: audit recorder", ErrIncompleteDependencies)
	}
	if depcheck.IsNil(deps.Logger) {
		return nil, fmt.Errorf("%w: logger", ErrIncompleteDependencies)
	}
	return &Service{
		reader: deps.Reader, projects: deps.Projects, writer: deps.Writer, authorizer: deps.Authorizer,
		stripeCredentials: deps.StripeCredentials, stripeGateway: deps.StripeGateway,
		stripeAPIKey: deps.StripeAPIKey, stripeWebhookSecret: deps.StripeWebhookSecret,
		audit: deps.Audit, logger: deps.Logger,
	}, nil
}

var (
	ErrInvalidTeam                 = errors.New("invoice team id must be positive")
	ErrInvalidPeriod               = errors.New("invoice period must end after it starts")
	ErrInvalidProject              = errors.New("invoice project id cannot be negative")
	ErrNoBillableTime              = appmodel.ErrNoBillableTime
	ErrAlreadyBilled               = appmodel.ErrAlreadyBilled
	ErrMixedCurrency               = appmodel.ErrMixedCurrency
	ErrInvoiceNotDraft             = appmodel.ErrInvoiceNotDraft
	ErrStripeSessionMismatch       = appmodel.ErrStripeSessionMismatch
	ErrStripeSessionPending        = appmodel.ErrStripeSessionPending
	ErrInvalidInvoice              = appmodel.ErrInvalidInvoice
	ErrInvalidClient               = appmodel.ErrInvalidClient
	ErrInvalidPaymentLink          = appmodel.ErrInvalidPaymentLink
	ErrHistoryConfirmationRequired = appmodel.ErrHistoryConfirmationRequired
	ErrInvalidReceipt              = appmodel.ErrInvalidReceipt
	ErrStripeUnavailable           = appmodel.ErrStripeUnavailable
	ErrInvalidStripeSignature      = appmodel.ErrInvalidStripeSignature
	ErrInvalidStripePayload        = appmodel.ErrInvalidStripePayload
	ErrInvalidStripeMetadata       = appmodel.ErrInvalidStripeMetadata
	ErrMissingStripePaymentStatus  = appmodel.ErrMissingStripePaymentStatus
)

func (s *Service) List(ctx context.Context, teamID int64) ([]appmodel.InvoiceSummaryResult, error) {
	if teamID <= 0 {
		return nil, ErrInvalidTeam
	}
	items, err := s.reader.ListInvoiceDetails(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("list invoices: %w", err)
	}
	summaries := make([]appmodel.InvoiceSummaryResult, 0, len(items))
	for _, item := range items {
		totalCents, totalHours, err := invoiceLineTotals(item.Lines)
		if err != nil {
			return nil, fmt.Errorf("summarize invoice %d: %w", item.Invoice.ID, err)
		}
		summaries = append(summaries, appmodel.InvoiceSummaryResult{
			Invoice: item.Invoice, TotalCents: totalCents, TotalHoursHundredths: totalHours,
		})
	}
	return summaries, nil
}

// BuildIndex assembles the invoice list, draft choices and billable history
// for the invoice index page.
func (s *Service) BuildIndex(ctx context.Context, request appmodel.InvoiceIndexRequest) (appmodel.InvoiceIndexSnapshot, error) {
	if request.TeamID <= 0 {
		return appmodel.InvoiceIndexSnapshot{}, ErrInvalidTeam
	}
	invoices, err := s.List(ctx, request.TeamID)
	if err != nil {
		return appmodel.InvoiceIndexSnapshot{}, fmt.Errorf("load invoice index: %w", err)
	}
	options, err := s.DraftOptions(ctx, request.TeamID)
	if err != nil {
		return appmodel.InvoiceIndexSnapshot{}, fmt.Errorf("load invoice draft options: %w", err)
	}
	unbilled, err := s.UnbilledProjectTime(ctx, request.TeamID, 0)
	if err != nil {
		return appmodel.InvoiceIndexSnapshot{}, fmt.Errorf("load unbilled invoice history: %w", err)
	}
	unassigned, err := s.UnassignedHistory(ctx, request.TeamID)
	if err != nil {
		return appmodel.InvoiceIndexSnapshot{}, fmt.Errorf("load unassigned invoice history: %w", err)
	}
	return appmodel.InvoiceIndexSnapshot{
		Invoices: invoices, DraftOptions: options, Unbilled: unbilled, Unassigned: unassigned,
	}, nil
}

// DraftOptions applies invoice eligibility rules and joins saved client details
// to the workspace's active project catalog for the invoice creation flow.
func (s *Service) DraftOptions(ctx context.Context, teamID int64) (appmodel.InvoiceDraftOptions, error) {
	if teamID <= 0 {
		return appmodel.InvoiceDraftOptions{}, ErrInvalidTeam
	}
	projects, err := s.projects.ListProjects(ctx, appmodel.ProjectCatalogQuery{TeamID: teamID})
	if err != nil {
		return appmodel.InvoiceDraftOptions{}, fmt.Errorf("list invoice projects: %w", err)
	}
	clients, err := s.projects.ListProjectClients(ctx, teamID)
	if err != nil {
		return appmodel.InvoiceDraftOptions{}, fmt.Errorf("load invoice project clients: %w", err)
	}
	options := appmodel.InvoiceDraftOptions{Projects: make([]appmodel.InvoiceProjectOption, 0, len(projects))}
	for _, project := range projects {
		hasRate := project.Billable && project.BillableRateCents != nil && *project.BillableRateCents > 0
		if hasRate {
			options.HasBillable = true
		}
		option := appmodel.InvoiceProjectOption{Project: project, Eligible: !project.Archived && hasRate}
		if client, ok := clients[project.ID]; ok {
			option.Client, option.HasClient = client, true
		}
		options.Projects = append(options.Projects, option)
	}
	return options, nil
}

// UnbilledProjectTime returns billable time not yet included on an invoice.
func (s *Service) UnbilledProjectTime(ctx context.Context, teamID, projectID int64) ([]model.UnbilledProject, error) {
	if teamID <= 0 || projectID < 0 {
		return nil, ErrInvalidInvoice
	}
	items, err := s.reader.Unbilled(ctx, teamID, projectID)
	if err != nil {
		return nil, fmt.Errorf("list unbilled project time: %w", err)
	}
	return items, nil
}

// UnassignedHistory returns activities whose historical time needs a project
// before it can be billed.
func (s *Service) UnassignedHistory(ctx context.Context, teamID int64) ([]model.UnassignedActivity, error) {
	if teamID <= 0 {
		return nil, ErrInvalidTeam
	}
	items, err := s.reader.UnassignedActivities(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("list unassigned invoice history: %w", err)
	}
	return items, nil
}

// OverlappingDocuments finds other invoices with the same line labels in an
// overlapping period. This is advisory and does not block invoice creation.
func (s *Service) OverlappingDocuments(ctx context.Context, teamID, exceptID int64, start, end time.Time, labels []string) ([]string, error) {
	if teamID <= 0 || exceptID <= 0 || start.IsZero() || !end.After(start) {
		return nil, ErrInvalidInvoice
	}
	numbers, err := s.reader.OverlappingInvoices(ctx, teamID, exceptID, start, end, labels)
	if err != nil {
		return nil, fmt.Errorf("find overlapping invoices: %w", err)
	}
	return numbers, nil
}

func (s *Service) Get(ctx context.Context, teamID, invoiceID int64) (appmodel.InvoiceDetailResult, error) {
	if teamID <= 0 || invoiceID <= 0 {
		return appmodel.InvoiceDetailResult{}, ErrInvalidInvoice
	}
	details, err := s.reader.GetInvoiceDetails(ctx, appmodel.InvoiceLookupQuery{TeamID: teamID, InvoiceID: invoiceID})
	if err != nil {
		return appmodel.InvoiceDetailResult{}, fmt.Errorf("get invoice details: %w", err)
	}
	totalCents, totalHours, err := invoiceLineTotals(details.Lines)
	if err != nil {
		return appmodel.InvoiceDetailResult{}, fmt.Errorf("summarize invoice %d: %w", details.Invoice.ID, err)
	}
	return appmodel.InvoiceDetailResult{
		Invoice: details.Invoice, Lines: details.Lines,
		TotalCents: totalCents, TotalHoursHundredths: totalHours,
	}, nil
}

func invoiceLineTotals(lines []model.InvoiceLine) (int, int, error) {
	totalCents, totalHours := 0, 0
	for _, line := range lines {
		var err error
		totalCents, err = money.AddCents(totalCents, line.AmountCents)
		if err != nil {
			return 0, 0, err
		}
		totalHours, err = money.AddInt(totalHours, money.HoursHundredths(line.Seconds))
		if err != nil {
			return 0, 0, err
		}
	}
	return totalCents, totalHours, nil
}

// CreateDraft snapshots billable time and commits the invoice, lines, and
// billing stamps through one persistence operation.
func (s *Service) CreateDraft(ctx context.Context, request appmodel.InvoiceDraftRequest) (model.Invoice, []model.InvoiceLine, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return model.Invoice{}, nil, ErrInvalidTeam
	}
	if !request.End.After(request.Start) {
		return model.Invoice{}, nil, ErrInvalidPeriod
	}
	if request.Options.ProjectID < 0 {
		return model.Invoice{}, nil, ErrInvalidProject
	}
	request.Options.RememberClient = request.Options.RememberClient && request.Options.ProjectID > 0
	inv, lines, err := s.writer.CreateInvoiceDraft(ctx, request)
	if err != nil {
		return model.Invoice{}, nil, fmt.Errorf("create invoice draft: %w", err)
	}
	return inv, lines, nil
}

// CreateDraftWithOverlapCheck completes the invoice creation use case while
// preserving the non-blocking nature of duplicate-document warnings.
func (s *Service) CreateDraftWithOverlapCheck(ctx context.Context, request appmodel.InvoiceDraftRequest) (DraftCreation, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return DraftCreation{}, ErrInvalidTeam
	}
	invoice, lines, err := s.CreateDraft(ctx, request)
	if err != nil {
		return DraftCreation{}, err
	}
	s.recordAudit(ctx, request.TeamID, request.CallerID, "invoice.create", invoice.Number, invoice.ClientName)
	labels := make([]string, 0, len(lines))
	for _, line := range lines {
		labels = append(labels, line.Label)
	}
	overlaps, advisoryErr := s.OverlappingDocuments(ctx, request.TeamID, invoice.ID, request.Start, request.End, labels)
	return DraftCreation{
		Invoice: invoice, Lines: lines, Overlaps: overlaps, AdvisoryError: advisoryErr,
	}, nil
}

// MarkSent moves a draft invoice to sent once. Duplicate or stale requests
// cannot move an already paid invoice backwards.
func (s *Service) MarkSent(ctx context.Context, request appmodel.InvoiceMutationRequest) error {
	if request.TeamID <= 0 {
		return ErrInvalidInvoice
	}
	if request.InvoiceID <= 0 || request.CallerID <= 0 {
		return ErrInvalidInvoice
	}
	if err := s.writer.MarkInvoiceSentOnce(ctx, request); err != nil {
		return fmt.Errorf("mark invoice sent: %w", err)
	}
	return nil
}

// RecordPayment applies a manual payment once and returns the persisted invoice
// number for audit and response mapping.
func (s *Service) RecordPayment(ctx context.Context, request appmodel.ManualPaymentRequest) (string, bool, error) {
	if request.TeamID <= 0 || request.InvoiceID <= 0 || request.CallerID <= 0 {
		return "", false, ErrInvalidInvoice
	}
	number, changed, err := s.writer.MarkInvoicePaidOnce(ctx, appmodel.InvoiceMutationRequest{
		TeamID: request.TeamID, InvoiceID: request.InvoiceID, CallerID: request.CallerID,
	})
	if err != nil {
		return "", false, fmt.Errorf("record invoice payment: %w", err)
	}
	return number, changed, nil
}

// RecordStripePayment applies a provider payment only when its checkout
// session matches the session stored on the invoice.
func (s *Service) recordStripePayment(ctx context.Context, teamID, invoiceID int64, sessionID string) (bool, error) {
	if teamID <= 0 || invoiceID <= 0 || sessionID == "" {
		return false, ErrInvalidInvoice
	}
	changed, err := s.writer.MarkInvoicePaidFromStripe(ctx, appmodel.InvoiceStripePaymentRequest{
		TeamID: teamID, InvoiceID: invoiceID, StripeSessionID: sessionID,
	})
	if err != nil {
		return false, fmt.Errorf("record Stripe invoice payment: %w", err)
	}
	return changed, nil
}

// RebuildDraft refreshes an invoice's lines from current billable time. The
// store rechecks draft status and replaces lines and session stamps atomically.
func (s *Service) RebuildDraft(ctx context.Context, request appmodel.InvoiceMutationRequest) error {
	if request.TeamID <= 0 {
		return ErrInvalidInvoice
	}
	if request.InvoiceID <= 0 || request.CallerID <= 0 {
		return ErrInvalidInvoice
	}
	if err := s.writer.RebuildInvoice(ctx, request); err != nil {
		return fmt.Errorf("rebuild invoice draft: %w", err)
	}
	s.recordAudit(ctx, request.TeamID, request.CallerID, "invoice.rebuild", strconv.FormatInt(request.InvoiceID, 10), "")
	return nil
}

// UpdateDraftDetails changes editable invoice metadata and remembered
// project-client defaults as one store operation. The store rechecks draft
// status to protect against a concurrent send.
func (s *Service) UpdateDraftDetails(ctx context.Context, request appmodel.InvoiceDraftUpdateRequest) error {
	if request.TeamID <= 0 || request.InvoiceID <= 0 || request.CallerID <= 0 {
		return ErrInvalidInvoice
	}
	request.Client = strings.TrimSpace(request.Client)
	if request.Client == "" {
		return ErrInvalidClient
	}
	current, err := s.reader.GetInvoiceDetails(ctx, appmodel.InvoiceLookupQuery{TeamID: request.TeamID, InvoiceID: request.InvoiceID})
	if err != nil {
		return fmt.Errorf("load invoice for edit: %w", err)
	}
	if current.Invoice.Status != "draft" {
		return model.ErrInvoiceNotDraft
	}
	request.ProjectID = current.Invoice.ProjectID
	request.Details, request.Email, request.Notes = strings.TrimSpace(request.Details), strings.TrimSpace(request.Email), strings.TrimSpace(request.Notes)
	if err := s.writer.UpdateInvoiceMetaAndProjectClient(ctx, request); err != nil {
		return fmt.Errorf("update invoice details: %w", err)
	}
	s.recordAudit(ctx, request.TeamID, request.CallerID, "invoice.edit", current.Invoice.Number, "")
	return nil
}

// DeleteDraft delegates the state check and deletion to one locked
// persistence operation, so sent or paid documents cannot be removed.
func (s *Service) DeleteDraft(ctx context.Context, request appmodel.InvoiceMutationRequest) error {
	if request.TeamID <= 0 {
		return ErrInvalidInvoice
	}
	if request.InvoiceID <= 0 || request.CallerID <= 0 {
		return ErrInvalidInvoice
	}
	if err := s.writer.DeleteInvoice(ctx, request); err != nil {
		return fmt.Errorf("delete invoice draft: %w", err)
	}
	return nil
}

// CreateStripePaymentLink creates a provider checkout session and persists its
// URL and session ID on the invoice. Credentials and provider protocol details
// remain inside the invoicing workflow and its outbound ports.
func (s *Service) recordAudit(ctx context.Context, teamID, actorID int64, action, target, meta string) {
	effectCtx, cancel := postcommit.NewContext(ctx)
	defer cancel()
	if err := s.audit.Record(effectCtx, model.AuditRecord{
		TeamID: teamID, UserID: actorID, Action: action, Target: target, Meta: meta, IP: requestctx.ClientIP(ctx),
	}); err != nil {
		s.logger.Printf("invoicing: record %s audit for team %d: %v", action, teamID, err)
	}
}

// AssignHistoryToBillableProject moves an activity's entire unbilled history
// onto a billable project. The user must explicitly acknowledge that scope.
func (s *Service) AssignHistoryToBillableProject(ctx context.Context, request appmodel.AssignBillableHistoryRequest) error {
	if request.TeamID <= 0 {
		return ErrInvalidInvoice
	}
	teamID, activityID, projectID, callerID, confirmed := request.TeamID, request.ActivityID, request.ProjectID, request.CallerID, request.Confirmed
	if activityID <= 0 || projectID <= 0 || callerID <= 0 {
		return ErrInvalidInvoice
	}
	if !confirmed {
		return ErrHistoryConfirmationRequired
	}
	if err := s.writer.AssignUnassignedActivityForBilling(ctx, appmodel.AssignActivityProjectRequest{
		TeamID: teamID, ActivityID: activityID, ProjectID: projectID, CallerID: callerID,
	}); err != nil {
		return fmt.Errorf("assign activity history for billing: %w", err)
	}
	s.recordAudit(ctx, teamID, callerID, "activity.project", strconv.FormatInt(activityID, 10), strconv.FormatInt(projectID, 10))
	return nil
}

// SetReceipt updates the optional payment receipt attached to an invoice and
// advances its revision so prepared outbound documents can detect the edit.
func (s *Service) SetReceipt(ctx context.Context, request appmodel.InvoiceReceiptRequest) error {
	if request.TeamID <= 0 {
		return ErrInvalidInvoice
	}
	invoiceID, callerID, receipt := request.InvoiceID, request.CallerID, request.Receipt
	if invoiceID <= 0 || callerID <= 0 {
		return ErrInvalidInvoice
	}
	receipt = strings.TrimSpace(receipt)
	if len(receipt) > 300 {
		return ErrInvalidReceipt
	}
	request.Receipt = receipt
	if err := s.writer.SetInvoiceReceipt(ctx, request); err != nil {
		return fmt.Errorf("set invoice receipt: %w", err)
	}
	return nil
}
