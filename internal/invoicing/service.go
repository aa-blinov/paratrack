// Package invoicing owns invoice creation workflows shared by HTTP and
// future non-HTTP adapters.
package invoicing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
)

// Reader exposes team-scoped invoice and billable-time queries.
type Reader interface {
	ListInvoiceDetails(context.Context, int64) ([]model.InvoiceDetails, error)
	GetInvoiceDetails(context.Context, appmodel.InvoiceLookupQuery) (model.InvoiceDetails, error)
	Unbilled(context.Context, appmodel.UnbilledProjectQuery) ([]model.UnbilledProject, error)
	UnassignedActivities(context.Context, int64) ([]model.UnassignedActivity, error)
	OverlappingInvoices(context.Context, appmodel.InvoiceOverlapQuery) ([]string, error)
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
