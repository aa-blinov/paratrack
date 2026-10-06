package push

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/pushport"
)

// topicStoreStub answers the topic-aware store calls the delivery policy makes
// and remembers what a caller asked to mute.
type topicStoreStub struct {
	pushStoreStub
	muted          map[int64][]string
	targetsErr     error
	saved          []string
	savedUser      int64
	savedCallers   []int64
	targetsQueries []appmodel.NotificationTargetsQuery
}

// The topic-aware store calls default to "nobody muted anything, everybody is
// a target", so the delivery tests that predate the setting keep their meaning.
func (*pushStoreStub) MutedNotificationTopics(context.Context, appmodel.NotificationTopicsQuery) ([]string, error) {
	return nil, nil
}

func (*pushStoreStub) SetMutedNotificationTopics(context.Context, appmodel.NotificationTopicsCommand) error {
	return nil
}

func (*pushStoreStub) NotificationTargets(_ context.Context, query appmodel.NotificationTargetsQuery) ([]int64, error) {
	return query.UserIDs, nil
}

func (s *topicStoreStub) MutedNotificationTopics(_ context.Context, query appmodel.NotificationTopicsQuery) ([]string, error) {
	if query.TeamID <= 0 || query.UserID <= 0 {
		return nil, appmodel.ErrInvalidNotificationTopic
	}
	return s.muted[query.UserID], nil
}

func (s *topicStoreStub) SetMutedNotificationTopics(_ context.Context, command appmodel.NotificationTopicsCommand) error {
	s.saved = append(s.saved, command.Topics...)
	s.savedUser = command.UserID
	s.savedCallers = append(s.savedCallers, command.CallerID)
	return nil
}

// ListPushSubscriptions honors the recipient list, like the real store does,
// so a test can tell "the topic was applied" from "the stub ignored it".
func (s *topicStoreStub) ListPushSubscriptions(_ context.Context, _ int64, userIDs ...int64) ([]pushport.Subscription, error) {
	if len(userIDs) == 0 {
		return s.subs, s.listErr
	}
	var out []pushport.Subscription
	for _, sub := range s.subs {
		for _, userID := range userIDs {
			if sub.UserID == userID {
				out = append(out, sub)
				break
			}
		}
	}
	return out, s.listErr
}

func (s *topicStoreStub) NotificationTargets(_ context.Context, query appmodel.NotificationTargetsQuery) ([]int64, error) {
	s.targetsQueries = append(s.targetsQueries, query)
	if s.targetsErr != nil {
		return nil, s.targetsErr
	}
	var out []int64
	for _, userID := range query.UserIDs {
		for _, topic := range s.muted[userID] {
			if topic == query.Topic {
				goto next
			}
		}
		out = append(out, userID)
	next:
	}
	return out, nil
}

func topicService(t *testing.T, store Store, sender Sender) *Service {
	t.Helper()
	service, err := New(store, sender, log.New(io.Discard, "", 0), pushAuditNoop{})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestTopicSelectionSkipsThePersonWhoMutedIt(t *testing.T) {
	store := &topicStoreStub{
		pushStoreStub: pushStoreStub{subs: []pushport.Subscription{{ID: 1, TeamID: 7, UserID: 4, Endpoint: "https://push.example/a"}}},
		muted:         map[int64][]string{4: {appmodel.NotificationTopicGoalAchieved}},
	}
	sender := &pushSenderStub{statuses: []int{http.StatusCreated}}
	service := topicService(t, store, sender)

	if _, err := service.deliver(t.Context(), 7, []int64{4}, Notification{Title: "Goal met", Topic: appmodel.NotificationTopicGoalAchieved}); err != nil {
		t.Fatal(err)
	}
	if len(sender.payloads) != 0 {
		t.Fatalf("muted topic still reached the device: %d sends", len(sender.payloads))
	}
	if len(store.targetsQueries) != 1 || store.targetsQueries[0].Topic != appmodel.NotificationTopicGoalAchieved {
		t.Fatalf("topic selection not applied: %+v", store.targetsQueries)
	}

	if _, err := service.deliver(t.Context(), 7, []int64{4}, Notification{Title: "Goal met", Topic: appmodel.NotificationTopicSessionStopped}); err != nil {
		t.Fatal(err)
	}
	if len(sender.payloads) != 1 {
		t.Fatalf("unmuted topic did not reach the device: %d sends", len(sender.payloads))
	}
}

func TestDeliveryWithoutATopicKeepsEveryRecipient(t *testing.T) {
	store := &topicStoreStub{
		pushStoreStub: pushStoreStub{subs: []pushport.Subscription{{ID: 1, TeamID: 7, UserID: 4, Endpoint: "https://push.example/a"}}},
		muted:         map[int64][]string{4: {appmodel.NotificationTopicGoalAchieved}},
	}
	sender := &pushSenderStub{statuses: []int{http.StatusCreated}}
	service := topicService(t, store, sender)

	if _, err := service.deliver(t.Context(), 7, []int64{4}, Notification{Title: "Invoice paid"}); err != nil {
		t.Fatal(err)
	}
	if len(sender.payloads) != 1 {
		t.Fatalf("producer without a topic was filtered: %d sends", len(sender.payloads))
	}
	if len(store.targetsQueries) != 0 {
		t.Fatalf("topic selection asked for: %+v", store.targetsQueries)
	}
}

func TestSaveMutedTopicsRefusesSomebodyElsesSelection(t *testing.T) {
	store := &topicStoreStub{}
	service := topicService(t, store, &pushSenderStub{statuses: []int{http.StatusCreated}})

	if err := service.SaveMutedTopics(t.Context(), appmodel.NotificationTopicsCommand{TeamID: 7, UserID: 4, CallerID: 5}); !errors.Is(err, appmodel.ErrForbidden) {
		t.Fatalf("mute for another person = %v, want forbidden", err)
	}
	if len(store.saved) != 0 {
		t.Fatalf("refused selection was stored: %v", store.saved)
	}
	if err := service.SaveMutedTopics(t.Context(), appmodel.NotificationTopicsCommand{TeamID: 7, UserID: 4, CallerID: 4, Topics: []string{appmodel.NotificationTopicPayrollPaid}}); err != nil {
		t.Fatalf("own selection = %v, want success", err)
	}
	if len(store.saved) != 1 || store.saved[0] != appmodel.NotificationTopicPayrollPaid || store.savedUser != 4 {
		t.Fatalf("stored selection = %v for user %d", store.saved, store.savedUser)
	}
}

func TestTestNotificationReportsAnAbsentChannel(t *testing.T) {
	// No subscription at all: the check must say the channel is not set up
	// instead of reporting a delivery that never happened.
	store := &topicStoreStub{}
	service := topicService(t, store, &pushSenderStub{statuses: []int{http.StatusCreated}})

	result, err := service.TestNotification(t.Context(), appmodel.NotificationTestRequest{TeamID: 7, UserID: 4, CallerID: 4, Title: "paratrack"})
	if err != nil {
		t.Fatalf("absent channel = %v, want a completed check", err)
	}
	if result.Delivered != 0 {
		t.Fatalf("delivered=%d without a subscribed device", result.Delivered)
	}
}

func TestTestNotificationReportsARefusedDelivery(t *testing.T) {
	store := &topicStoreStub{
		pushStoreStub: pushStoreStub{subs: []pushport.Subscription{{ID: 1, TeamID: 7, UserID: 4, Endpoint: "https://push.example/a"}}},
	}
	sender := &pushSenderStub{err: errors.New("push service refused")}
	service := topicService(t, store, sender)

	result, err := service.TestNotification(t.Context(), appmodel.NotificationTestRequest{TeamID: 7, UserID: 4, CallerID: 4, Title: "paratrack"})
	if !errors.Is(err, appmodel.ErrNotificationNotDelivered) {
		t.Fatalf("refused delivery = %v, want a channel answer the screen can name", err)
	}
	if result.Delivered != 0 {
		t.Fatalf("delivered=%d after a refusal", result.Delivered)
	}
}

func TestTestNotificationDeliversToTheCallersOwnDevices(t *testing.T) {
	store := &topicStoreStub{
		pushStoreStub: pushStoreStub{subs: []pushport.Subscription{{ID: 1, TeamID: 7, UserID: 4, Endpoint: "https://push.example/a"}}},
		// Even a fully muted selection must not hide the channel check: the
		// person asked whether notifications work at all.
		muted: map[int64][]string{4: {appmodel.NotificationTopicSessionStopped}},
	}
	sender := &pushSenderStub{statuses: []int{http.StatusCreated}}
	service := topicService(t, store, sender)

	result, err := service.TestNotification(t.Context(), appmodel.NotificationTestRequest{TeamID: 7, UserID: 4, CallerID: 4, Title: "paratrack", Body: "check", URL: "/settings/notifications"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Delivered != 1 || len(sender.payloads) != 1 {
		t.Fatalf("delivered=%d sends=%d, want one accepted delivery", result.Delivered, len(sender.payloads))
	}
	if _, err := service.TestNotification(t.Context(), appmodel.NotificationTestRequest{TeamID: 7, UserID: 4, CallerID: 5, Title: "paratrack"}); !errors.Is(err, appmodel.ErrForbidden) {
		t.Fatalf("check for another person = %v, want forbidden", err)
	}
}
