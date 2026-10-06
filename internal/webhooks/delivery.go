package webhooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/postcommit"
	"github.com/aa-blinov/paratrack/internal/webhookport"
)

type deliveryPayload struct {
	Event  webhookport.EventName `json:"event"`
	TeamID int64                 `json:"team_id"`
	SentAt string                `json:"sent_at"`
	// Action marks a delivery the manager asked for by hand. It is omitted from
	// live events, so a receiver sees the production envelope unchanged and can
	// still tell a synthetic sample apart by this field.
	Action string          `json:"action,omitempty"`
	Data   json.RawMessage `json:"data"`
}

// dispatchEvent fans a committed event out to its snapshotted endpoints.
// Source event IDs make this safe to replay after a worker crash.
func (s *Service) dispatchEvent(ctx context.Context, sourceEventID, teamID int64, event webhookport.Event, sentAt time.Time, snapshot []int64) error {
	if sourceEventID <= 0 || teamID <= 0 || depcheck.IsNil(event) {
		return ErrInvalidWebhook
	}
	eventName := event.EventName()
	if strings.TrimSpace(string(eventName)) == "" {
		return ErrInvalidWebhook
	}
	s.mu.Lock()
	stopping := s.stopping
	s.mu.Unlock()
	if stopping {
		return ErrShuttingDown
	}
	hooks, err := s.store.ListWebhooks(ctx, appmodel.WebhookListQuery{TeamID: teamID})
	if err != nil {
		return fmt.Errorf("list webhook endpoints: %w", err)
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode webhook event data: %w", err)
	}
	body, err := json.Marshal(deliveryPayload{
		Event: eventName, TeamID: teamID,
		SentAt: sentAt.UTC().Format(time.RFC3339), Data: json.RawMessage(payload),
	})
	if err != nil {
		return fmt.Errorf("encode webhook event: %w", err)
	}
	var targets []webhookport.Webhook
	allowed := make(map[int64]bool, len(snapshot))
	for _, id := range snapshot {
		allowed[id] = true
	}
	for _, hook := range hooks {
		if hook.Active && allowed[hook.ID] {
			targets = append(targets, hook)
		}
	}
	if len(targets) == 0 {
		return nil
	}

	webhookIDs := make([]int64, 0, len(targets))
	for _, hook := range targets {
		webhookIDs = append(webhookIDs, hook.ID)
	}
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return ErrShuttingDown
	}
	s.dispatches.Add(1)
	s.mu.Unlock()
	defer s.dispatches.Done()
	if err := s.store.EnqueueWebhookDeliveries(ctx, appmodel.WebhookDeliveryBatchRequest{
		SourceEventID: sourceEventID, TeamID: teamID, WebhookIDs: webhookIDs, Event: string(eventName), Payload: body,
	}); err != nil {
		return fmt.Errorf("enqueue webhook delivery batch: %w", err)
	}
	for range min(len(targets), cap(s.wake)) {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
	return nil
}

// Run starts independent event fan-out and endpoint delivery pools. Separate
// pools keep a sustained event backlog from starving remote deliveries.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	if ctx == nil {
		s.logger.Printf("webhooks: worker not started: nil context")
		return
	}
	if interval <= 0 {
		interval = time.Second
	}
	s.mu.Lock()
	if s.stopping || s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	workerCtx, cancel := context.WithCancel(ctx)
	s.runCancel = cancel
	s.runDone = make(chan struct{})
	done := s.runDone
	s.mu.Unlock()
	defer close(done)

	var workers sync.WaitGroup
	workers.Add(eventWorkerCount + deliveryWorkerCount)
	for range eventWorkerCount {
		go func() {
			defer workers.Done()
			s.runEventWorker(workerCtx, interval)
		}()
	}
	for range deliveryWorkerCount {
		go func() {
			defer workers.Done()
			s.runDeliveryWorker(workerCtx, interval)
		}()
	}
	workers.Wait()
}

func (s *Service) runEventWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		event, ok, err := s.store.ClaimWebhookEvent(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.logger.Printf("webhooks: claim event: %v", err)
		}
		if ok {
			s.processQueuedEvent(ctx, event)
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) runDeliveryWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		job, ok, err := s.store.ClaimWebhookDelivery(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.logger.Printf("webhooks: claim delivery: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-s.wake:
			case <-ticker.C:
			}
			continue
		}
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-s.wake:
			case <-ticker.C:
			}
			continue
		}
		s.deliverQueued(ctx, job)
	}
}

func (s *Service) processQueuedEvent(ctx context.Context, job webhookport.CommittedEvent) {
	event, err := webhookport.DecodeEvent(webhookport.EventName(job.Event), job.Payload)
	if err == nil {
		err = s.dispatchEvent(ctx, job.ID, job.TeamID, event, job.CreatedAt, job.WebhookIDs)
	}
	persistCtx, cancel := postcommit.NewContextWithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err != nil {
		delay := 5 * time.Second * time.Duration(1<<min(job.Attempts-1, 8))
		if delay > 30*time.Minute {
			delay = 30 * time.Minute
		}
		if retryErr := s.store.RetryWebhookEvent(persistCtx, webhookport.EventRetryRequest{Event: job, AvailableAt: s.now().UTC().Add(delay)}); retryErr != nil {
			s.logger.Printf("webhooks: retry event %d: %v (processing: %v)", job.ID, retryErr, err)
		}
		return
	}
	if err := s.store.CompleteWebhookEvent(persistCtx, job); err != nil {
		s.logger.Printf("webhooks: complete event %d: %v", job.ID, err)
	}
}

func (s *Service) deliverQueued(ctx context.Context, job webhookport.DeliveryJob) {
	deliveryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	status, _, err := s.deliverAttempt(deliveryCtx, webhookport.Webhook{
		ID: job.WebhookID, TeamID: job.TeamID, URL: job.URL, Secret: job.Secret,
	}, job.Event, job.Payload)
	cancel()
	retry := !errors.Is(err, ErrPrivateTarget) && (err != nil || status == 429 || status >= 500)
	persistCtx, persistCancel := postcommit.NewContextWithTimeout(ctx, 5*time.Second)
	defer persistCancel()
	if retry {
		delay := 5 * time.Second * time.Duration(1<<min(job.Attempts-1, 8))
		if delay > 30*time.Minute {
			delay = 30 * time.Minute
		}
		if retryErr := s.store.RetryWebhookDelivery(persistCtx, webhookport.DeliveryRetryRequest{Job: job, AvailableAt: s.now().UTC().Add(delay), MaxAttempts: deliveryMaxAttempts}); retryErr != nil {
			s.logger.Printf("webhooks: schedule retry for delivery %d: %v (delivery: %v)", job.ID, retryErr, err)
		}
		return
	}
	if completeErr := s.store.CompleteWebhookDelivery(persistCtx, job); completeErr != nil {
		// The target may have accepted the request. Leave the lease to expire so
		// another worker can reconcile it with at-least-once delivery semantics.
		s.logger.Printf("webhooks: complete delivery %d: %v", job.ID, completeErr)
	}
}

// Shutdown stops accepting events and cancels the delivery pollers. Unclaimed
// rows remain durable; active leases expire for another process to reclaim.
func (s *Service) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return ErrNilShutdownContext
	}
	s.mu.Lock()
	s.stopping = true
	done, cancel := s.runDone, s.runCancel
	if cancel != nil {
		cancel()
	}
	s.mu.Unlock()
	s.dispatches.Wait()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		<-done // do not release the database while claimed deliveries use it
		return ctx.Err()
	}
}

// deliverWithBackoff sends a signed delivery and retries transient failures.
func (s *Service) deliverWithBackoff(ctx context.Context, hook webhookport.Webhook, event string, body []byte, backoff []time.Duration) {
	for attempt := 0; ; attempt++ {
		status, _, err := s.deliverAttempt(ctx, hook, event, body)
		retry := !errors.Is(err, ErrPrivateTarget) && (err != nil || status == 429 || status >= 500)
		if !retry || attempt >= len(backoff) {
			return
		}
		timer := time.NewTimer(backoff[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// deliverAttempt signs and sends one request, then records what came back. The
// same path serves queued deliveries and the manager's test run, so an
// integrator verifies the signature and payload production actually sends.
func (s *Service) deliverAttempt(ctx context.Context, hook webhookport.Webhook, event string, body []byte) (int, string, error) {
	ts := fmt.Sprint(s.now().Unix())
	request := DeliveryRequest{
		URL: hook.URL, Event: event, Timestamp: ts,
		Signature:   SignPayload(hook.Secret, body),
		SignatureV2: SignPayload(hook.Secret, append([]byte(ts+"."), body...)),
		Body:        body,
	}
	var (
		status       int
		responseBody string
		err          error
	)
	// Adapters that can return the receiver's answer let the history show it;
	// the plain delivery port still works, just with nothing to show.
	if rich, ok := s.deliverer.(responseDeliverer); ok {
		status, responseBody, err = rich.DeliverWithResponse(ctx, request)
	} else {
		status, err = s.deliverer.Deliver(ctx, request)
	}
	message := ""
	if err != nil {
		// Delivery summaries are visible to team members. Do not persist network
		// and adapter details, which may contain the destination URL.
		message = deliveryFailureMessage(err)
	}
	s.logDelivery(ctx, hook.ID, event, status, message, string(body), responseBody)
	return status, responseBody, err
}

func deliveryFailureMessage(err error) string {
	if errors.Is(err, ErrPrivateTarget) {
		return "destination is not allowed"
	}
	return "delivery failed"
}

func (s *Service) logDelivery(ctx context.Context, webhookID int64, event string, status int, message, requestBody, responseBody string) {
	if ctx == nil {
		s.logger.Printf("webhooks: delivery result for endpoint %d event %s cannot be persisted without a context", webhookID, event)
		return
	}
	logCtx, cancel := postcommit.NewContextWithTimeout(ctx, deliveryLogTimeout)
	defer cancel()
	if err := s.store.LogWebhookDelivery(logCtx, appmodel.WebhookDeliveryLogRequest{
		WebhookID: webhookID, Event: event, Status: status, Error: message,
		RequestBody: requestBody, ResponseBody: responseBody,
	}); err != nil {
		s.logger.Printf("webhooks: log delivery for endpoint %d event %s: %v", webhookID, event, err)
	}
}

func Subscribes(events, event string) bool {
	return webhookport.Subscribes(events, webhookport.EventName(event))
}

func SignPayload(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
