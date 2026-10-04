// Package push owns subscription and notification-delivery policy.
package push

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	"github.com/aa-blinov/paratrack/internal/pushport"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

var (
	ErrInvalidSubscription    = appmodel.ErrInvalidPushSubscription
	ErrIncompleteDependencies = errors.New("push service dependencies are incomplete")
	ErrDeliveryQueueFull      = errors.New("push delivery queue is full")
	ErrServiceStopping        = errors.New("push service is stopping")
	ErrNilShutdownContext     = errors.New("push shutdown context is nil")
)

const deliveryQueueCapacity = 128

type Store interface {
	EnsureVAPIDKeys(context.Context) (string, string, error)
	UpsertPushSubscription(context.Context, appmodel.PushSubscribeRequest) error
	ListPushSubscriptions(context.Context, int64, ...int64) ([]pushport.Subscription, error)
	CountPushSubscriptions(context.Context, int64) (int, error)
	DeletePushSubscription(context.Context, appmodel.PushUnsubscribeRequest) error
	DeletePushSubscriptionForCleanup(context.Context, appmodel.PushSubscriptionCleanupRequest) error
}

// Sender owns Web Push protocol details and outbound network policy.
type Sender = pushport.Sender
type SendResult = pushport.SendResult

type Logger interface {
	Printf(string, ...any)
}

type AuditRecorder interface {
	Record(context.Context, model.AuditRecord) error
}

type Notification struct {
	Title string
	Body  string
	URL   string
}

// EnqueueNotificationRequest identifies the workspace, recipients, and
// payload for one asynchronous delivery batch.
type EnqueueNotificationRequest struct {
	TeamID       int64
	UserIDs      []int64
	Notification Notification
}

type pushPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	Tag   string `json:"tag"`
}

type Service struct {
	store  Store
	sender Sender
	logger Logger
	audit  AuditRecorder
	mu     sync.Mutex
	queue  chan deliveryJob
	cancel context.CancelFunc
	done   chan struct{}
	closed bool
}

// EnqueueNotification is the asynchronous delivery surface used by HTTP.
func (s *Service) EnqueueNotification(request EnqueueNotificationRequest) error {
	return s.enqueue(request.TeamID, request.UserIDs, request.Notification)
}

type deliveryJob struct {
	teamID       int64
	userIDs      []int64
	notification Notification
}

func New(store Store, sender Sender, logger Logger, audit AuditRecorder) (*Service, error) {
	if depcheck.IsNil(store) {
		return nil, ErrIncompleteDependencies
	}
	if depcheck.IsNil(sender) {
		return nil, fmt.Errorf("%w: sender", ErrIncompleteDependencies)
	}
	if depcheck.IsNil(logger) {
		return nil, fmt.Errorf("%w: logger", ErrIncompleteDependencies)
	}
	if depcheck.IsNil(audit) {
		return nil, fmt.Errorf("%w: audit recorder", ErrIncompleteDependencies)
	}
	return &Service{store: store, sender: sender, logger: logger, audit: audit}, nil
}

// Enqueue schedules a notification without holding up the caller.
func (s *Service) enqueue(teamID int64, userIDs []int64, notification Notification) error {
	if teamID <= 0 || len(userIDs) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrServiceStopping
	}
	if s.queue == nil {
		ctx, cancel := context.WithCancel(context.Background())
		s.queue = make(chan deliveryJob, deliveryQueueCapacity)
		s.cancel = cancel
		s.done = make(chan struct{})
		go s.run(ctx)
	}
	job := deliveryJob{teamID: teamID, userIDs: append([]int64(nil), userIDs...), notification: notification}
	select {
	case s.queue <- job:
		return nil
	default:
		return ErrDeliveryQueueFull
	}
}

func (s *Service) run(ctx context.Context) {
	defer close(s.done)
	for {
		var job deliveryJob
		select {
		case <-ctx.Done():
			return
		case next, ok := <-s.queue:
			if !ok {
				return
			}
			job = next
		}
		deliveryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if err := s.notify(deliveryCtx, job.teamID, job.userIDs, job.notification); err != nil && ctx.Err() == nil {
			s.logger.Printf("push: deliver notification to team %d: %v", job.teamID, err)
		}
		cancel()
	}
}

// Shutdown stops accepting notifications, drains queued deliveries, and
// cancels an in-flight send if the caller's shutdown deadline expires.
func (s *Service) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return ErrNilShutdownContext
	}
	s.mu.Lock()
	if s.closed {
		done := s.done
		s.mu.Unlock()
		if done == nil {
			return nil
		}
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.closed = true
	if s.queue == nil {
		s.mu.Unlock()
		return nil
	}
	close(s.queue)
	done, cancel := s.done, s.cancel
	s.mu.Unlock()
	select {
	case <-done:
		cancel()
		return nil
	case <-ctx.Done():
		cancel()
		<-done
		return ctx.Err()
	}
}

// Notify sends a message to the active endpoints for the selected users.
// Endpoint delivery failures are isolated; dead endpoints are removed and the
// remaining recipients still receive the notification.
func (s *Service) notify(ctx context.Context, teamID int64, userIDs []int64, notification Notification) error {
	if teamID <= 0 || len(userIDs) == 0 {
		return nil
	}
	subs, err := s.subscriptions(ctx, teamID, userIDs...)
	if err != nil {
		return err
	}
	if len(subs) == 0 {
		return nil
	}
	public, private, err := s.vapidKeys(ctx)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(pushPayload{
		Title: notification.Title,
		Body:  notification.Body,
		URL:   notification.URL,
		Tag:   "paratrack-" + strings.ReplaceAll(notification.Title, " ", "-"),
	})
	if err != nil {
		return fmt.Errorf("encode Web Push payload: %w", err)
	}
	var deliveryErrors []error
	for _, sub := range subs {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(deliveryErrors, err)...)
		}
		result, err := s.sender.Send(ctx, sub, public, private, payload)
		if err != nil {
			deliveryErrors = append(deliveryErrors, fmt.Errorf("send Web Push to subscription %d: %w", sub.ID, err))
			continue
		}
		if result.SubscriptionExpired {
			if err := s.removeExpired(ctx, appmodel.PushSubscriptionCleanupRequest{TeamID: sub.TeamID, UserID: sub.UserID, Endpoint: sub.Endpoint}); err != nil {
				deliveryErrors = append(deliveryErrors, fmt.Errorf("remove expired Web Push subscription %d: %w", sub.ID, err))
			}
		}
	}
	return errors.Join(deliveryErrors...)
}

func (s *Service) PublicKey(ctx context.Context) (string, error) {
	public, _, err := s.store.EnsureVAPIDKeys(ctx)
	if err != nil {
		return "", fmt.Errorf("load Web Push public key: %w", err)
	}
	return public, nil
}

func (s *Service) vapidKeys(ctx context.Context) (string, string, error) {
	public, private, err := s.store.EnsureVAPIDKeys(ctx)
	if err != nil {
		return "", "", fmt.Errorf("load Web Push keys: %w", err)
	}
	return public, private, nil
}

func (s *Service) Subscribe(ctx context.Context, request appmodel.PushSubscribeRequest) error {
	request.Endpoint = strings.TrimSpace(request.Endpoint)
	request.PublicKey = strings.TrimSpace(request.PublicKey)
	request.AuthSecret = strings.TrimSpace(request.AuthSecret)
	parsed, err := url.Parse(request.Endpoint)
	if request.TeamID <= 0 || request.UserID <= 0 || request.CallerID <= 0 {
		return ErrInvalidSubscription
	}
	if request.CallerID != request.UserID {
		return model.ErrForbidden
	}
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return ErrInvalidSubscription
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(request.PublicKey)
	if err != nil || len(publicKey) != 65 || publicKey[0] != 4 {
		return ErrInvalidSubscription
	}
	authSecret, err := base64.RawURLEncoding.DecodeString(request.AuthSecret)
	if err != nil || len(authSecret) != 16 {
		return ErrInvalidSubscription
	}
	if err := s.store.UpsertPushSubscription(ctx, request); err != nil {
		return fmt.Errorf("save push subscription: %w", err)
	}
	s.recordAudit(ctx, request.TeamID, request.UserID, "push.subscribe")
	return nil
}

// UnsubscribeForMember is the user initiated operation exposed to transports.
// Internal endpoint cleanup uses its own maintenance command without audit noise.
func (s *Service) UnsubscribeForMember(ctx context.Context, request appmodel.PushUnsubscribeRequest) error {
	if request.TeamID <= 0 || request.UserID <= 0 || request.CallerID <= 0 {
		return ErrInvalidSubscription
	}
	if request.CallerID != request.UserID {
		return model.ErrForbidden
	}
	if err := s.unsubscribeMember(ctx, request); err != nil {
		return err
	}
	s.recordAudit(ctx, request.TeamID, request.UserID, "push.unsubscribe")
	return nil
}

func (s *Service) unsubscribeMember(ctx context.Context, request appmodel.PushUnsubscribeRequest) error {
	request.Endpoint = strings.TrimSpace(request.Endpoint)
	if request.TeamID <= 0 || request.UserID <= 0 || request.CallerID != request.UserID || request.Endpoint == "" {
		return ErrInvalidSubscription
	}
	if err := s.store.DeletePushSubscription(ctx, request); err != nil {
		return fmt.Errorf("delete push subscription: %w", err)
	}
	return nil
}

func (s *Service) removeExpired(ctx context.Context, request appmodel.PushSubscriptionCleanupRequest) error {
	request.Endpoint = strings.TrimSpace(request.Endpoint)
	if request.TeamID <= 0 || request.UserID <= 0 || request.Endpoint == "" {
		return ErrInvalidSubscription
	}
	if err := s.store.DeletePushSubscriptionForCleanup(ctx, request); err != nil {
		return fmt.Errorf("delete expired push subscription: %w", err)
	}
	return nil
}

func (s *Service) recordAudit(ctx context.Context, teamID, actorID int64, action string) {
	effectCtx, cancel := postcommit.NewContext(ctx)
	defer cancel()
	if err := s.audit.Record(effectCtx, model.AuditRecord{
		TeamID: teamID, UserID: actorID, Action: action, IP: requestctx.ClientIP(ctx),
	}); err != nil {
		s.logger.Printf("push: record %s audit for team %d: %v", action, teamID, err)
	}
}

func (s *Service) subscriptions(ctx context.Context, teamID int64, userIDs ...int64) ([]pushport.Subscription, error) {
	if teamID <= 0 {
		return nil, model.ErrNotFound
	}
	subs, err := s.store.ListPushSubscriptions(ctx, teamID, userIDs...)
	if err != nil {
		return nil, fmt.Errorf("list push subscriptions: %w", err)
	}
	return subs, nil
}

// SubscriptionCount returns the device count without exposing endpoint keys
// or authentication material to a transport adapter.
func (s *Service) SubscriptionCount(ctx context.Context, teamID int64) (int, error) {
	if teamID <= 0 {
		return 0, model.ErrNotFound
	}
	count, err := s.store.CountPushSubscriptions(ctx, teamID)
	if err != nil {
		return 0, fmt.Errorf("count push subscriptions: %w", err)
	}
	return count, nil
}
