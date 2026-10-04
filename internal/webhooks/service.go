// Package webhooks owns endpoint registration, event selection, signatures
// and delivery retry policy. HTTP transport is supplied through Deliverer.
package webhooks

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/postcommit"
	"github.com/aa-blinov/paratrack/internal/requestctx"
	"github.com/aa-blinov/paratrack/internal/webhookport"
)

var (
	ErrInvalidWebhook         = appmodel.ErrInvalidWebhook
	ErrShuttingDown           = errors.New("webhook service is shutting down")
	ErrIncompleteDependencies = errors.New("webhook service dependencies are incomplete")
	ErrNilShutdownContext     = errors.New("webhook shutdown context is nil")
)

const deliveryLogTimeout = 5 * time.Second

var knownEvents = map[webhookport.EventName]struct{}{
	webhookport.EventSessionStarted: {}, webhookport.EventSessionStopped: {}, webhookport.EventInvoiceCreated: {},
	webhookport.EventInvoicePaid: {}, webhookport.EventPaymentLinkCreated: {}, webhookport.EventImportCompleted: {},
	"*": {},
}

const defaultEvents = string(webhookport.EventSessionStopped) + "," + string(webhookport.EventInvoiceCreated)

const (
	eventWorkerCount    = 2
	deliveryWorkerCount = 8
	deliveryMaxAttempts = 10
)

type Store interface {
	CreateWebhook(context.Context, appmodel.WebhookRegistrationCommand) (webhookport.WebhookSummary, error)
	DeleteWebhook(context.Context, appmodel.WebhookDeleteRequest) error
	ListWebhookSummaries(context.Context, int64) ([]webhookport.WebhookSummary, error)
	ListWebhooks(context.Context, int64) ([]webhookport.Webhook, error)
	ListRecentWebhookDeliveries(context.Context, int64, int) (map[int64][]webhookport.WebhookDeliverySummary, error)
	LogWebhookDelivery(context.Context, appmodel.WebhookDeliveryLogRequest) error
	EnqueueWebhookDeliveries(context.Context, appmodel.WebhookDeliveryBatchRequest) error
	ClaimWebhookEvent(context.Context) (webhookport.CommittedEvent, bool, error)
	CompleteWebhookEvent(context.Context, webhookport.CommittedEvent) error
	RetryWebhookEvent(context.Context, appmodel.WebhookEventRetryRequest) error
	ClaimWebhookDelivery(context.Context) (webhookport.DeliveryJob, bool, error)
	CompleteWebhookDelivery(context.Context, webhookport.DeliveryJob) error
	RetryWebhookDelivery(context.Context, appmodel.WebhookDeliveryRetryRequest) error
}

type DeliveryRequest = webhookport.DeliveryRequest
type Deliverer = webhookport.Deliverer

type AuditRecorder interface {
	Record(context.Context, model.AuditRecord) error
}

type Logger interface {
	Printf(string, ...any)
}

type Service struct {
	store      Store
	deliverer  Deliverer
	audit      AuditRecorder
	logger     Logger
	now        func() time.Time
	backoff    []time.Duration
	mu         sync.Mutex
	dispatches sync.WaitGroup
	stopping   bool
	started    bool
	runCancel  context.CancelFunc
	runDone    chan struct{}
	wake       chan struct{}
}

func New(store Store, deliverer Deliverer, now func() time.Time, audit AuditRecorder, logger Logger) (*Service, error) {
	if depcheck.IsNil(store) {
		return nil, fmt.Errorf("%w: store", ErrIncompleteDependencies)
	}
	if depcheck.IsNil(deliverer) {
		return nil, fmt.Errorf("%w: delivery adapter", ErrIncompleteDependencies)
	}
	if now == nil {
		return nil, fmt.Errorf("%w: clock", ErrIncompleteDependencies)
	}
	if depcheck.IsNil(audit) {
		return nil, fmt.Errorf("%w: audit recorder", ErrIncompleteDependencies)
	}
	if depcheck.IsNil(logger) {
		return nil, fmt.Errorf("%w: logger", ErrIncompleteDependencies)
	}
	return &Service{
		store: store, deliverer: deliverer, now: now, audit: audit, logger: logger,
		backoff: []time.Duration{2 * time.Second, 10 * time.Second, 60 * time.Second},
		wake:    make(chan struct{}, deliveryWorkerCount),
	}, nil
}

var ErrPrivateTarget = webhookport.ErrPrivateTarget

func (s *Service) Create(ctx context.Context, request appmodel.WebhookCreateRequest) (webhookport.WebhookSummary, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return webhookport.WebhookSummary{}, fmt.Errorf("%w: team and caller IDs must be positive", ErrInvalidWebhook)
	}
	parsed, err := url.Parse(strings.TrimSpace(request.URL))
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || parsed.User != nil {
		return webhookport.WebhookSummary{}, fmt.Errorf("%w: URL must be an http(s) endpoint", ErrInvalidWebhook)
	}
	secret := strings.TrimSpace(request.Secret)
	if secret == "" {
		return webhookport.WebhookSummary{}, fmt.Errorf("%w: signing secret is required", ErrInvalidWebhook)
	}
	events, err := normalizeEvents(request.Events)
	if err != nil {
		return webhookport.WebhookSummary{}, err
	}
	webhook, err := s.store.CreateWebhook(ctx, appmodel.WebhookRegistrationCommand{
		TeamID: request.TeamID, CallerID: request.CallerID,
		URL: parsed.String(), Secret: secret, Events: events,
	})
	if err != nil {
		return webhookport.WebhookSummary{}, fmt.Errorf("create webhook: %w", err)
	}
	s.recordAudit(ctx, request.TeamID, request.CallerID, "webhook.create", fmt.Sprintf("%d", webhook.ID), webhook.URL)
	return webhook, nil
}

func normalizeEvents(values []string) (string, error) {
	var selected []string
	seen := make(map[string]bool)
	for _, value := range values {
		for _, raw := range strings.Split(value, ",") {
			event := strings.TrimSpace(raw)
			if event == "" {
				continue
			}
			if _, ok := knownEvents[webhookport.EventName(event)]; !ok {
				return "", fmt.Errorf("%w: unsupported event %q", ErrInvalidWebhook, event)
			}
			if !seen[event] {
				seen[event] = true
				selected = append(selected, event)
			}
		}
	}
	if len(selected) == 0 {
		return defaultEvents, nil
	}
	return strings.Join(selected, ","), nil
}

func (s *Service) Delete(ctx context.Context, request appmodel.WebhookDeleteRequest) error {
	if request.TeamID <= 0 {
		return ErrInvalidWebhook
	}
	teamID, callerID, webhookID := request.TeamID, request.CallerID, request.WebhookID
	if callerID <= 0 || webhookID <= 0 {
		return ErrInvalidWebhook
	}
	if err := s.store.DeleteWebhook(ctx, request); err != nil {
		return fmt.Errorf("delete webhook: %w", err)
	}
	s.recordAudit(ctx, teamID, callerID, "webhook.delete", fmt.Sprintf("%d", webhookID), "")
	return nil
}

func (s *Service) recordAudit(ctx context.Context, teamID, actorID int64, action, target, meta string) {
	effectCtx, cancel := postcommit.NewContext(ctx)
	defer cancel()
	if err := s.audit.Record(effectCtx, model.AuditRecord{
		TeamID: teamID, UserID: actorID, Action: action, Target: target, Meta: meta, IP: requestctx.ClientIP(ctx),
	}); err != nil {
		s.logger.Printf("webhooks: record %s audit for team %d: %v", action, teamID, err)
	}
}

func (s *Service) List(ctx context.Context, teamID int64) ([]webhookport.WebhookSummary, error) {
	if teamID <= 0 {
		return nil, ErrInvalidWebhook
	}
	hooks, err := s.store.ListWebhookSummaries(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("list webhooks: %w", err)
	}
	return hooks, nil
}

// RecentDeliveries loads a bounded number of newest attempts for every
// endpoint in a workspace with one store query.
func (s *Service) RecentDeliveries(ctx context.Context, teamID int64, perWebhook int) (map[int64][]webhookport.WebhookDeliverySummary, error) {
	if teamID <= 0 || perWebhook <= 0 {
		return nil, ErrInvalidWebhook
	}
	deliveries, err := s.store.ListRecentWebhookDeliveries(ctx, teamID, perWebhook)
	if err != nil {
		return nil, fmt.Errorf("list recent webhook deliveries: %w", err)
	}
	return deliveries, nil
}
