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
	ListWebhookSummaries(context.Context, appmodel.WebhookListQuery) ([]webhookport.WebhookSummary, error)
	ListWebhooks(context.Context, appmodel.WebhookListQuery) ([]webhookport.Webhook, error)
	ListRecentWebhookDeliveries(context.Context, appmodel.WebhookDeliveryHistoryQuery) (map[int64][]webhookport.WebhookDeliverySummary, error)
	LogWebhookDelivery(context.Context, appmodel.WebhookDeliveryLogRequest) error
	EnqueueWebhookDeliveries(context.Context, appmodel.WebhookDeliveryBatchRequest) error
	ClaimWebhookEvent(context.Context) (webhookport.CommittedEvent, bool, error)
	CompleteWebhookEvent(context.Context, webhookport.CommittedEvent) error
	RetryWebhookEvent(context.Context, webhookport.EventRetryRequest) error
	ClaimWebhookDelivery(context.Context) (webhookport.DeliveryJob, bool, error)
	CompleteWebhookDelivery(context.Context, webhookport.DeliveryJob) error
	RetryWebhookDelivery(context.Context, webhookport.DeliveryRetryRequest) error
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

func (s *Service) List(ctx context.Context, query appmodel.WebhookListQuery) ([]webhookport.WebhookSummary, error) {
	if query.TeamID <= 0 {
		return nil, ErrInvalidWebhook
	}
	hooks, err := s.store.ListWebhookSummaries(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list webhooks: %w", err)
	}
	return hooks, nil
}

// RecentDeliveries loads a bounded number of newest attempts for every
// endpoint in a workspace with one store query.
func (s *Service) RecentDeliveries(ctx context.Context, query appmodel.WebhookDeliveryHistoryQuery) (map[int64][]webhookport.WebhookDeliverySummary, error) {
	if query.TeamID <= 0 || query.Limit <= 0 {
		return nil, ErrInvalidWebhook
	}
	deliveries, err := s.store.ListRecentWebhookDeliveries(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list recent webhook deliveries: %w", err)
	}
	return deliveries, nil
}

// Management assembles the credential-free endpoints and their recent
// delivery history for the settings page.
func (s *Service) Management(ctx context.Context, query appmodel.WebhookManagementQuery) (appmodel.WebhookManagementSnapshot, error) {
	if query.TeamID <= 0 || query.DeliveriesPerEndpoint <= 0 {
		return appmodel.WebhookManagementSnapshot{}, ErrInvalidWebhook
	}
	endpoints, err := s.List(ctx, appmodel.WebhookListQuery{TeamID: query.TeamID})
	if err != nil {
		return appmodel.WebhookManagementSnapshot{}, err
	}
	deliveries, err := s.RecentDeliveries(ctx, appmodel.WebhookDeliveryHistoryQuery{TeamID: query.TeamID, Limit: query.DeliveriesPerEndpoint})
	if err != nil {
		return appmodel.WebhookManagementSnapshot{}, err
	}
	snapshot := appmodel.WebhookManagementSnapshot{
		Endpoints:  make([]appmodel.WebhookEndpointView, 0, len(endpoints)),
		Deliveries: make(map[int64][]appmodel.WebhookDeliveryView, len(deliveries)),
	}
	for _, endpoint := range endpoints {
		snapshot.Endpoints = append(snapshot.Endpoints, appmodel.WebhookEndpointView{
			ID: endpoint.ID, URL: endpoint.URL, Events: endpoint.Events, Active: endpoint.Active,
		})
	}
	for endpointID, attempts := range deliveries {
		views := make([]appmodel.WebhookDeliveryView, 0, len(attempts))
		for _, attempt := range attempts {
			views = append(views, appmodel.WebhookDeliveryView{
				CreatedAt: attempt.CreatedAt, Event: attempt.Event,
				Status: attempt.Status, Error: attempt.Error,
			})
		}
		snapshot.Deliveries[endpointID] = views
	}
	return snapshot, nil
}
