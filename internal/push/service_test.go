package push

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/pushport"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

type pushStoreStub struct {
	subs      []pushport.Subscription
	removed   []string
	keyReads  int
	listReads int
	countTeam int64
	listErr   error
	countErr  error
	removeErr error
}

type blockingPushStore struct {
	pushStoreStub
	entered chan struct{}
	release chan struct{}
}

func (s *blockingPushStore) ListPushSubscriptions(ctx context.Context, _ int64, _ ...int64) ([]pushport.Subscription, error) {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	select {
	case <-s.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *pushStoreStub) EnsureVAPIDKeys(context.Context) (string, string, error) {
	s.keyReads++
	return "public", "private", nil
}

func (*pushStoreStub) UpsertPushSubscription(context.Context, appmodel.PushSubscribeRequest) error {
	return nil
}

func (s *pushStoreStub) ListPushSubscriptions(context.Context, int64, ...int64) ([]pushport.Subscription, error) {
	s.listReads++
	return s.subs, s.listErr
}

func (s *pushStoreStub) CountPushSubscriptions(_ context.Context, teamID int64) (int, error) {
	s.countTeam = teamID
	if s.countErr != nil {
		return 0, s.countErr
	}
	return len(s.subs), nil
}

func TestSubscriptionCountUsesScopedCountOperation(t *testing.T) {
	store := &pushStoreStub{subs: []pushport.Subscription{{TeamID: 7, Endpoint: "https://push.example/device", P256DH: "key", Auth: "secret"}}}
	service, err := New(store, &pushSenderStub{}, log.New(io.Discard, "", 0), pushAuditNoop{})
	if err != nil {
		t.Fatal(err)
	}

	count, err := service.SubscriptionCount(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || store.countTeam != 7 || store.listReads != 0 {
		t.Fatalf("count=%d countTeam=%d listReads=%d, want scoped count and no credential list", count, store.countTeam, store.listReads)
	}
}

func (s *pushStoreStub) DeletePushSubscription(_ context.Context, request appmodel.PushUnsubscribeRequest) error {
	s.removed = append(s.removed, request.Endpoint)
	return s.removeErr
}

func (s *pushStoreStub) DeletePushSubscriptionForCleanup(_ context.Context, request appmodel.PushSubscriptionCleanupRequest) error {
	s.removed = append(s.removed, request.Endpoint)
	return s.removeErr
}

type pushSenderStub struct {
	statuses []int
	payloads [][]byte
	keys     [][2]string
	err      error
}

type pushAuditNoop struct{}

func (pushAuditNoop) Record(context.Context, model.AuditRecord) error {
	return nil
}

type pushAuditRecord struct {
	team, actor int64
	action, ip  string
}

type pushAuditSpy struct{ records []pushAuditRecord }

func (s *pushAuditSpy) Record(_ context.Context, record model.AuditRecord) error {
	s.records = append(s.records, pushAuditRecord{record.TeamID, record.UserID, record.Action, record.IP})
	return nil
}

func TestMemberSubscriptionMutationsAuditOnlyUserActions(t *testing.T) {
	audit := &pushAuditSpy{}
	service, err := New(&pushStoreStub{}, &pushSenderStub{}, log.New(io.Discard, "", 0), audit)
	if err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithClientIP(requestctx.WithActor(context.Background(), 8), "203.0.113.10")
	publicKey := make([]byte, 65)
	publicKey[0] = 4
	authSecret := make([]byte, 16)
	if err := service.Subscribe(ctx, appmodel.PushSubscribeRequest{
		TeamID: 5, UserID: 8, CallerID: 8, Endpoint: "https://push.example/device",
		PublicKey: base64.RawURLEncoding.EncodeToString(publicKey), AuthSecret: base64.RawURLEncoding.EncodeToString(authSecret),
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.UnsubscribeForMember(ctx, appmodel.PushUnsubscribeRequest{TeamID: 5, UserID: 8, CallerID: 8, Endpoint: "https://push.example/device"}); err != nil {
		t.Fatal(err)
	}
	if err := service.removeExpired(ctx, appmodel.PushSubscriptionCleanupRequest{TeamID: 5, UserID: 8, Endpoint: "https://push.example/expired"}); err != nil {
		t.Fatal(err)
	}
	if len(audit.records) != 2 {
		t.Fatalf("audit records = %+v, want only the two member actions", audit.records)
	}
	for i, action := range []string{"push.subscribe", "push.unsubscribe"} {
		got := audit.records[i]
		if got.team != 5 || got.actor != 8 || got.action != action || got.ip != "203.0.113.10" {
			t.Errorf("audit[%d] = %+v, want team=5 actor=8 action=%q ip=203.0.113.10", i, got, action)
		}
	}
}

func (s *pushSenderStub) Send(_ context.Context, _ pushport.Subscription, public, private string, payload []byte) (SendResult, error) {
	s.payloads = append(s.payloads, append([]byte(nil), payload...))
	s.keys = append(s.keys, [2]string{public, private})
	if s.err != nil {
		return SendResult{}, s.err
	}
	status := s.statuses[0]
	s.statuses = s.statuses[1:]
	return SendResult{SubscriptionExpired: status == http.StatusGone || status == http.StatusNotFound}, nil
}

func TestNotifySendsPayloadAndRemovesExpiredEndpoints(t *testing.T) {
	store := &pushStoreStub{subs: []pushport.Subscription{
		{ID: 1, TeamID: 4, UserID: 7, Endpoint: "https://push.example/old"},
		{ID: 2, TeamID: 4, UserID: 8, Endpoint: "https://push.example/current"},
	}}
	sender := &pushSenderStub{statuses: []int{http.StatusGone, http.StatusCreated}}
	service, err := New(store, sender, log.New(io.Discard, "", 0), pushAuditNoop{})
	if err != nil {
		t.Fatal(err)
	}
	err = service.notify(context.Background(), 4, []int64{7, 8}, Notification{
		Title: "Session stopped", Body: "Deep work finished", URL: "/stats",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sender.payloads) != 2 || len(store.removed) != 1 || store.removed[0] != "https://push.example/old" {
		t.Fatalf("sent=%d removed=%v", len(sender.payloads), store.removed)
	}
	for _, keys := range sender.keys {
		if keys != [2]string{"public", "private"} {
			t.Fatalf("VAPID keys = %v", keys)
		}
	}
	var payload map[string]string
	if err := json.Unmarshal(sender.payloads[0], &payload); err != nil {
		t.Fatal(err)
	}
	if payload["title"] != "Session stopped" || payload["body"] != "Deep work finished" || payload["url"] != "/stats" || payload["tag"] != "paratrack-Session-stopped" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestNotifyKeepsDeliveringAfterEndpointFailure(t *testing.T) {
	store := &pushStoreStub{subs: []pushport.Subscription{{ID: 1}, {ID: 2}}}
	sender := &pushSenderStub{statuses: []int{http.StatusCreated, http.StatusCreated}, err: errors.New("push provider unavailable")}
	service, err := New(store, sender, log.New(io.Discard, "", 0), pushAuditNoop{})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.notify(context.Background(), 4, []int64{7}, Notification{}); err == nil {
		t.Fatal("provider errors were not reported")
	}
	if len(sender.payloads) != 2 {
		t.Fatalf("attempted sends = %d, want both subscriptions", len(sender.payloads))
	}
}

func TestEnqueueNotificationBoundsQueueAndShutdownDrains(t *testing.T) {
	store := &blockingPushStore{entered: make(chan struct{}, 1), release: make(chan struct{})}
	service, err := New(store, &pushSenderStub{}, log.New(io.Discard, "", 0), pushAuditNoop{})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.EnqueueNotification(EnqueueNotificationRequest{TeamID: 4, UserIDs: []int64{7}, Notification: Notification{Title: "first"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-store.entered:
	case <-time.After(time.Second):
		t.Fatal("delivery worker did not start")
	}
	for i := 0; i < deliveryQueueCapacity; i++ {
		if err := service.EnqueueNotification(EnqueueNotificationRequest{TeamID: 4, UserIDs: []int64{7}}); err != nil {
			t.Fatalf("enqueue within queue capacity: %v", err)
		}
	}
	if err := service.EnqueueNotification(EnqueueNotificationRequest{TeamID: 4, UserIDs: []int64{7}}); !errors.Is(err, ErrDeliveryQueueFull) {
		t.Fatalf("enqueue beyond queue capacity = %v, want ErrDeliveryQueueFull", err)
	}
	close(store.release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := service.EnqueueNotification(EnqueueNotificationRequest{TeamID: 4, UserIDs: []int64{7}}); !errors.Is(err, ErrServiceStopping) {
		t.Fatalf("enqueue after shutdown = %v, want ErrServiceStopping", err)
	}
}
