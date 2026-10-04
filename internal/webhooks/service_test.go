package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
	"github.com/aa-blinov/paratrack/internal/webhookport"
)

type noopAudit struct{}

func (noopAudit) Record(context.Context, model.AuditRecord) error {
	return nil
}

type noopLogger struct{}

func (noopLogger) Printf(string, ...any) {}

func TestDeliveryFailureMessageDoesNotExposeAdapterDetails(t *testing.T) {
	const secretURL = "https://hooks.example.test/callback?token=top-secret"
	if got := deliveryFailureMessage(errors.New("dial " + secretURL)); got != "delivery failed" {
		t.Fatalf("generic delivery message = %q, want safe category", got)
	}
	if got := deliveryFailureMessage(ErrPrivateTarget); got != "destination is not allowed" {
		t.Fatalf("private-target message = %q, want safe category", got)
	}
}

type auditRecord struct {
	team, actor              int64
	action, target, meta, ip string
}

type recordingAudit struct{ records []auditRecord }

func (s *recordingAudit) Record(_ context.Context, record model.AuditRecord) error {
	s.records = append(s.records, auditRecord{record.TeamID, record.UserID, record.Action, record.Target, record.Meta, record.IP})
	return nil
}

func testService(store Store, deliverer Deliverer, now func() time.Time) (*Service, error) {
	return New(store, deliverer, now, noopAudit{}, noopLogger{})
}

type lifecycleStore struct {
	logged chan struct{}
	outbox *fakeWebhookOutbox
}

type webhookManagementStore struct {
	lifecycleStore
	listTeamID, deliveryTeamID int64
	deliveryLimit              int
}

type oversizedStore struct{ lifecycleStore }

type contextRecordingStore struct {
	lifecycleStore
	logged chan error
}

type eventBacklogStore struct {
	lifecycleStore
	mu              sync.Mutex
	nextEventID     int64
	deliveryClaimed bool
}

type cancelDeliverer struct{ cancel context.CancelFunc }

type capturingDeliverer struct{ request *DeliveryRequest }

type fakeWebhookOutbox struct {
	mu           sync.Mutex
	jobs         []webhookport.DeliveryJob
	enqueueCalls int
}

type deliveryRequestCapture chan DeliveryRequest

func (c deliveryRequestCapture) Deliver(_ context.Context, request DeliveryRequest) (int, error) {
	c <- request
	return 204, nil
}

func (d cancelDeliverer) Deliver(ctx context.Context, _ DeliveryRequest) (int, error) {
	d.cancel()
	return 0, ctx.Err()
}

func (d *capturingDeliverer) Deliver(_ context.Context, req DeliveryRequest) (int, error) {
	d.request = &req
	return 204, nil
}

func (oversizedStore) ListWebhookSummaries(context.Context, int64) ([]webhookport.WebhookSummary, error) {
	return nil, nil
}
func (oversizedStore) ListWebhooks(context.Context, int64) ([]webhookport.Webhook, error) {
	hooks := make([]webhookport.Webhook, 257)
	for i := range hooks {
		hooks[i] = webhookport.Webhook{ID: int64(i + 1), URL: "https://example.test/hook", Events: "*", Active: true}
	}
	return hooks, nil
}

func (lifecycleStore) CreateWebhook(_ context.Context, request appmodel.WebhookRegistrationCommand) (webhookport.WebhookSummary, error) {
	return webhookport.WebhookSummary{ID: 3, TeamID: request.TeamID, URL: request.URL, Events: request.Events, Active: true}, nil
}
func (lifecycleStore) DeleteWebhook(context.Context, appmodel.WebhookDeleteRequest) error { return nil }
func (lifecycleStore) ListWebhookSummaries(context.Context, int64) ([]webhookport.WebhookSummary, error) {
	return []webhookport.WebhookSummary{{ID: 1, URL: "https://example.test/hook", Events: "*", Active: true}}, nil
}
func (lifecycleStore) ListWebhooks(context.Context, int64) ([]webhookport.Webhook, error) {
	return []webhookport.Webhook{{ID: 1, URL: "https://example.test/hook", Secret: "secret", Events: "*", Active: true}}, nil
}
func (lifecycleStore) ListRecentWebhookDeliveries(context.Context, int64, int) (map[int64][]webhookport.WebhookDeliverySummary, error) {
	return map[int64][]webhookport.WebhookDeliverySummary{}, nil
}

func (s *webhookManagementStore) ListWebhookSummaries(_ context.Context, teamID int64) ([]webhookport.WebhookSummary, error) {
	s.listTeamID = teamID
	return []webhookport.WebhookSummary{{ID: 3, TeamID: teamID, URL: "https://example.test/hook", Events: "*", Active: true}}, nil
}

func (s *webhookManagementStore) ListRecentWebhookDeliveries(_ context.Context, teamID int64, limit int) (map[int64][]webhookport.WebhookDeliverySummary, error) {
	s.deliveryTeamID, s.deliveryLimit = teamID, limit
	return map[int64][]webhookport.WebhookDeliverySummary{
		3: {{CreatedAt: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC), Event: "session.stopped", Status: 204}},
	}, nil
}
func (s lifecycleStore) EnqueueWebhookDeliveries(_ context.Context, request appmodel.WebhookDeliveryBatchRequest) error {
	if s.outbox == nil {
		return nil
	}
	s.outbox.mu.Lock()
	defer s.outbox.mu.Unlock()
	s.outbox.enqueueCalls++
	for _, webhookID := range request.WebhookIDs {
		s.outbox.jobs = append(s.outbox.jobs, webhookport.DeliveryJob{
			ID: int64(len(s.outbox.jobs) + 1), TeamID: request.TeamID, WebhookID: webhookID,
			URL: "https://example.test/hook", Secret: "secret", Event: request.Event,
			Payload: append([]byte(nil), request.Payload...),
		})
	}
	return nil
}

func (lifecycleStore) ClaimWebhookEvent(context.Context) (webhookport.CommittedEvent, bool, error) {
	return webhookport.CommittedEvent{}, false, nil
}
func (lifecycleStore) CompleteWebhookEvent(context.Context, webhookport.CommittedEvent) error {
	return nil
}
func (lifecycleStore) RetryWebhookEvent(context.Context, webhookport.EventRetryRequest) error {
	return nil
}
func (s lifecycleStore) ClaimWebhookDelivery(context.Context) (webhookport.DeliveryJob, bool, error) {
	if s.outbox == nil {
		return webhookport.DeliveryJob{}, false, nil
	}
	s.outbox.mu.Lock()
	defer s.outbox.mu.Unlock()
	if len(s.outbox.jobs) == 0 {
		return webhookport.DeliveryJob{}, false, nil
	}
	job := s.outbox.jobs[0]
	s.outbox.jobs = s.outbox.jobs[1:]
	job.Attempts++
	job.Lease = "lease"
	return job, true, nil
}

func (s *eventBacklogStore) ClaimWebhookEvent(ctx context.Context) (webhookport.CommittedEvent, bool, error) {
	if err := ctx.Err(); err != nil {
		return webhookport.CommittedEvent{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextEventID++
	return webhookport.CommittedEvent{
		ID: s.nextEventID, TeamID: 7, Event: string(webhookport.EventSessionStarted),
		Payload:    []byte(`{"session_id":42,"activity":"Deep work"}`),
		WebhookIDs: []int64{1}, Attempts: 1, Lease: "event-lease", CreatedAt: time.Now(),
	}, true, nil
}

func (s *eventBacklogStore) ClaimWebhookDelivery(ctx context.Context) (webhookport.DeliveryJob, bool, error) {
	if err := ctx.Err(); err != nil {
		return webhookport.DeliveryJob{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deliveryClaimed {
		return webhookport.DeliveryJob{}, false, nil
	}
	s.deliveryClaimed = true
	return webhookport.DeliveryJob{
		ID: 1, TeamID: 7, WebhookID: 1, URL: "https://example.test/hook",
		Secret: "secret", Event: string(webhookport.EventSessionStarted),
		Payload: []byte(`{"event":"session.started"}`), Attempts: 1, Lease: "delivery-lease",
	}, true, nil
}
func (lifecycleStore) CompleteWebhookDelivery(context.Context, webhookport.DeliveryJob) error {
	return nil
}
func (lifecycleStore) RetryWebhookDelivery(context.Context, webhookport.DeliveryRetryRequest) error {
	return nil
}
func (s lifecycleStore) LogWebhookDelivery(context.Context, appmodel.WebhookDeliveryLogRequest) error {
	s.logged <- struct{}{}
	return nil
}

func runWebhookWorker(t *testing.T, service *Service) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		service.Run(ctx, time.Hour)
	}()
	t.Cleanup(func() {
		_ = service.Shutdown(context.Background())
		cancel()
		<-done
	})
}

func (s contextRecordingStore) LogWebhookDelivery(ctx context.Context, _ appmodel.WebhookDeliveryLogRequest) error {
	s.logged <- ctx.Err()
	return nil
}

type blockingDeliverer struct {
	started chan struct{}
	release chan struct{}
}

func (d blockingDeliverer) Deliver(context.Context, DeliveryRequest) (int, error) {
	close(d.started)
	<-d.release
	return 204, nil
}

func TestManagementMethodsReturnCredentialFreeSummaries(t *testing.T) {
	service, err := testService(lifecycleStore{logged: make(chan struct{}, 1)}, &capturingDeliverer{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}

	created, err := service.Create(context.Background(), appmodel.WebhookCreateRequest{
		TeamID: 7, CallerID: 11, URL: "https://example.test/hook", Secret: "signing-secret", Events: []string{"session.started"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != 3 || created.TeamID != 7 || created.URL != "https://example.test/hook" || created.Events != "session.started" || !created.Active {
		t.Fatalf("created summary = %+v", created)
	}

	listed, err := service.List(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != 1 || listed[0].URL != "https://example.test/hook" {
		t.Fatalf("listed summaries = %+v", listed)
	}
}

func TestWebhookManagementAssemblesScopedPageSnapshot(t *testing.T) {
	store := &webhookManagementStore{}
	service, err := testService(store, &capturingDeliverer{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.Management(context.Background(), 17)
	if err != nil {
		t.Fatalf("load webhook management snapshot: %v", err)
	}
	if store.listTeamID != 17 || store.deliveryTeamID != 17 || store.deliveryLimit != 5 {
		t.Fatalf("management queries: list team=%d deliveries team=%d limit=%d", store.listTeamID, store.deliveryTeamID, store.deliveryLimit)
	}
	if len(snapshot.Endpoints) != 1 || snapshot.Endpoints[0].ID != 3 || snapshot.Endpoints[0].URL != "https://example.test/hook" || !snapshot.Endpoints[0].Active {
		t.Fatalf("management endpoints = %+v", snapshot.Endpoints)
	}
	deliveries := snapshot.Deliveries[3]
	if len(deliveries) != 1 || deliveries[0].Event != "session.stopped" || deliveries[0].Status != 204 || deliveries[0].CreatedAt.IsZero() {
		t.Fatalf("management deliveries = %+v", deliveries)
	}
}

func TestEventWorkerFanoutSerializesTypedEventContract(t *testing.T) {
	requests := make(deliveryRequestCapture, 1)
	service, err := testService(lifecycleStore{logged: make(chan struct{}, 1), outbox: &fakeWebhookOutbox{}}, requests, func() time.Time {
		return time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	runWebhookWorker(t, service)

	if err := service.dispatchEvent(context.Background(), 41, 7, webhookport.InvoicePaidEvent{InvoiceID: 12, TeamID: 7}, time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), []int64{1}); err != nil {
		t.Fatalf("dispatchEvent: %v", err)
	}
	var request DeliveryRequest
	select {
	case request = <-requests:
	case <-time.After(time.Second):
		t.Fatal("webhook delivery did not start")
	}
	var envelope struct {
		Event  webhookport.EventName `json:"event"`
		TeamID int64                 `json:"team_id"`
		Data   json.RawMessage       `json:"data"`
	}
	if err := json.Unmarshal(request.Body, &envelope); err != nil {
		t.Fatalf("decode webhook envelope: %v", err)
	}
	wantData, err := json.Marshal(webhookport.InvoicePaidEvent{InvoiceID: 12, TeamID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Event != webhookport.EventInvoicePaid || envelope.TeamID != 7 || !bytes.Equal(envelope.Data, wantData) {
		t.Fatalf("webhook envelope = %+v with data %s, want event %q, team 7, data %s", envelope, envelope.Data, webhookport.EventInvoicePaid, wantData)
	}
}

func TestEventBacklogDoesNotStarveDeliveryWorkers(t *testing.T) {
	requests := make(deliveryRequestCapture, 1)
	store := &eventBacklogStore{lifecycleStore: lifecycleStore{
		logged: make(chan struct{}, 1),
	}}
	service, err := testService(store, requests, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	runWebhookWorker(t, service)
	select {
	case <-requests:
	case <-time.After(time.Second):
		t.Fatal("delivery worker starved by continuously available outbox events")
	}
}

func TestManagementMutationsRecordAuditAfterSuccess(t *testing.T) {
	audit := &recordingAudit{}
	service, err := New(lifecycleStore{logged: make(chan struct{}, 1)}, &capturingDeliverer{}, time.Now, audit, noopLogger{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithClientIP(context.Background(), "203.0.113.9")
	if _, err := service.Create(ctx, appmodel.WebhookCreateRequest{
		TeamID: 7, CallerID: 11, URL: "https://example.test/hook", Secret: "secret", Events: []string{"session.started"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(ctx, appmodel.WebhookDeleteRequest{TeamID: 7, CallerID: 11, WebhookID: 3}); err != nil {
		t.Fatal(err)
	}
	want := []auditRecord{
		{team: 7, actor: 11, action: "webhook.create", target: "3", meta: "https://example.test/hook", ip: "203.0.113.9"},
		{team: 7, actor: 11, action: "webhook.delete", target: "3", ip: "203.0.113.9"},
	}
	if !reflect.DeepEqual(audit.records, want) {
		t.Fatalf("audit records = %+v, want %+v", audit.records, want)
	}
}

func TestShutdownWaitsForDispatchedDeliveries(t *testing.T) {
	store := lifecycleStore{logged: make(chan struct{}, 1), outbox: &fakeWebhookOutbox{}}
	doer := blockingDeliverer{started: make(chan struct{}), release: make(chan struct{})}
	service, err := testService(store, doer, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	runWebhookWorker(t, service)
	if err := service.dispatchEvent(context.Background(), 42, 7, webhookport.SessionStartedEvent{SessionID: 3}, time.Now(), []int64{1}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-doer.started:
	case <-time.After(time.Second):
		t.Fatal("delivery did not start")
	}

	shutdown := make(chan error, 1)
	go func() { shutdown <- service.Shutdown(context.Background()) }()
	select {
	case err := <-shutdown:
		t.Fatalf("shutdown returned before delivery completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(doer.release)
	select {
	case err := <-shutdown:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish after delivery")
	}
	select {
	case <-store.logged:
	default:
		t.Fatal("delivery result was not recorded before shutdown returned")
	}
}

func TestDispatchPersistsBatchLargerThanFormerInMemoryCapacity(t *testing.T) {
	store := oversizedStore{lifecycleStore: lifecycleStore{outbox: &fakeWebhookOutbox{}}}
	service, err := testService(store, blockingDeliverer{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := make([]int64, 257)
	for i := range snapshot {
		snapshot[i] = int64(i + 1)
	}
	err = service.dispatchEvent(context.Background(), 43, 7, webhookport.SessionStartedEvent{SessionID: 3}, time.Now(), snapshot)
	if err != nil {
		t.Fatalf("Dispatch error = %v", err)
	}
	store.outbox.mu.Lock()
	enqueueCalls := store.outbox.enqueueCalls
	store.outbox.mu.Unlock()
	if enqueueCalls != 1 {
		t.Fatalf("batch was persisted %d times, want 1", enqueueCalls)
	}
	if err := service.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown after rejected batch: %v", err)
	}
}

func TestDeliveryResultLoggingSurvivesRequestCancellation(t *testing.T) {
	store := contextRecordingStore{
		lifecycleStore: lifecycleStore{logged: make(chan struct{}, 1)},
		logged:         make(chan error, 1),
	}
	requestCtx, cancel := context.WithCancel(context.Background())
	service, err := testService(store, cancelDeliverer{cancel: cancel}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	service.deliverWithBackoff(requestCtx, webhookport.Webhook{ID: 1, URL: "https://example.test/hook"}, "session.started", []byte(`{}`), nil)
	if err := <-store.logged; err != nil {
		t.Fatalf("delivery log context was canceled: %v", err)
	}
	cancel()
}

func TestDeliverySignatureTimestampUsesInjectedClock(t *testing.T) {
	store := lifecycleStore{logged: make(chan struct{}, 1)}
	doer := &capturingDeliverer{}
	now := time.Date(2025, time.April, 5, 6, 7, 8, 0, time.UTC)
	service, err := testService(store, doer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	service.deliverWithBackoff(context.Background(), webhookport.Webhook{
		ID: 1, URL: "https://example.test/hook", Secret: "secret",
	}, "session.started", []byte(`{}`), nil)
	if got, want := doer.request.Timestamp, "1743833228"; got != want {
		t.Fatalf("signature timestamp = %q, want %q", got, want)
	}
}
