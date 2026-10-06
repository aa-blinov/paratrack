package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/webhookport"
)

// testRunStore answers the endpoint lookup with a chosen subscription and keeps
// the delivery log so the history can be inspected.
type testRunStore struct {
	lifecycleStore
	hook webhookport.Webhook
	err  error
	logs []appmodel.WebhookDeliveryLogRequest
}

func (s *testRunStore) GetWebhookForDelivery(_ context.Context, request appmodel.WebhookLookupCommand) (webhookport.Webhook, error) {
	if s.err != nil {
		return webhookport.Webhook{}, s.err
	}
	hook := s.hook
	hook.TeamID, hook.ID = request.TeamID, request.WebhookID
	return hook, nil
}

func (s *testRunStore) LogWebhookDelivery(_ context.Context, request appmodel.WebhookDeliveryLogRequest) error {
	s.logs = append(s.logs, request)
	return nil
}

// answeringDeliverer replies like a real receiver, including a body, and also
// satisfies the optional richer delivery port.
type answeringDeliverer struct {
	status  int
	body    string
	request DeliveryRequest
}

func (d *answeringDeliverer) Deliver(_ context.Context, request DeliveryRequest) (int, error) {
	d.request = request
	return d.status, nil
}

func (d *answeringDeliverer) DeliverWithResponse(_ context.Context, request DeliveryRequest) (int, string, error) {
	d.request = request
	return d.status, d.body, nil
}

func newTestRunService(t *testing.T, hook webhookport.Webhook) (*Service, *testRunStore, *answeringDeliverer) {
	t.Helper()
	store := &testRunStore{
		lifecycleStore: lifecycleStore{outbox: &fakeWebhookOutbox{}},
		hook:           hook,
	}
	deliverer := &answeringDeliverer{status: 200, body: `{"received":true}`}
	service, err := testService(store, deliverer, func() time.Time {
		return time.Date(2026, time.October, 6, 12, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, store, deliverer
}

func subscribedTestHook() webhookport.Webhook {
	return webhookport.Webhook{
		ID: 1, URL: "https://hooks.example.test/events", Secret: "signing-secret",
		Events: "session.stopped,invoice.created", Active: true,
	}
}

// A test run must be indistinguishable from a live delivery except for the
// marker: same envelope, same signature, same headers.
func TestTestRunSendsTheSignedProductionEnvelope(t *testing.T) {
	service, store, deliverer := newTestRunService(t, subscribedTestHook())

	result, err := service.SendTest(context.Background(), appmodel.WebhookTestRequest{TeamID: 7, CallerID: 11, WebhookID: 1})
	if err != nil {
		t.Fatal(err)
	}
	sent := deliverer.request
	if sent.URL != "https://hooks.example.test/events" || sent.Event != "session.stopped" {
		t.Fatalf("request = %+v, want the first subscribed event on the endpoint URL", sent)
	}
	if len(store.logs) != 1 {
		t.Fatalf("delivery log rows = %d, want the test attempt in the history", len(store.logs))
	}
	if store.logs[0].RequestBody != string(sent.Body) {
		t.Fatalf("logged request body %q differs from the sent body %q", store.logs[0].RequestBody, sent.Body)
	}
	body := sent.Body
	if !strings.Contains(string(body), `"action":"test"`) {
		t.Fatalf("test body is not marked as a test: %s", body)
	}
	var envelope struct {
		Event  string          `json:"event"`
		TeamID int64           `json:"team_id"`
		SentAt string          `json:"sent_at"`
		Action string          `json:"action"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("test body is not the delivery envelope: %v", err)
	}
	if envelope.Event != "session.stopped" || envelope.TeamID != 7 || envelope.SentAt != "2026-10-06T12:00:00Z" {
		t.Fatalf("envelope = %+v, want the live envelope fields", envelope)
	}
	var data webhookport.SessionStoppedEvent
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatalf("test data does not decode as the chosen event: %v", err)
	}
	if data.SessionID != 0 || data.ActivityID != 0 || data.Start != "2026-10-06T12:00:00Z" {
		t.Fatalf("sample data = %+v, want identifiers that point at no real record", data)
	}
	timestamp := fmt.Sprint(time.Date(2026, time.October, 6, 12, 0, 0, 0, time.UTC).Unix())
	if sent.Timestamp != timestamp {
		t.Fatalf("timestamp = %q, want the unix seconds of the injected clock", sent.Timestamp)
	}
	if sent.Signature != SignPayload("signing-secret", body) {
		t.Fatal("test request was not signed with the endpoint secret over the exact body")
	}
	if sent.SignatureV2 != SignPayload("signing-secret", append([]byte(timestamp+"."), body...)) {
		t.Fatal("test request replay signature does not cover timestamp and body")
	}
	if result.Status != 200 || result.ResponseBody != `{"received":true}` || result.Error != "" {
		t.Fatalf("result = %+v, want the receiver's answer", result)
	}
	if store.logs[0].Status != 200 || store.logs[0].ResponseBody != `{"received":true}` {
		t.Fatalf("delivery log = %+v, want the status and response body of the attempt", store.logs[0])
	}
}

// A live delivery has no marker: the added field must stay out of the wire
// format, or integrators see a contract change they never asked for.
func TestLiveDeliveryBodyCarriesNoTestMarker(t *testing.T) {
	store := lifecycleStore{logged: make(chan struct{}, 1), outbox: &fakeWebhookOutbox{}}
	deliverer := &answeringDeliverer{status: 204}
	service, err := testService(store, deliverer, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.dispatchEvent(context.Background(), 42, 7, webhookport.SessionStartedEvent{SessionID: 3}, time.Now(), []int64{1}); err != nil {
		t.Fatal(err)
	}
	runWebhookWorker(t, service)
	<-store.logged
	if strings.Contains(string(deliverer.request.Body), "action") {
		t.Fatalf("live body carries a test marker: %s", deliverer.request.Body)
	}
}

func TestTestRunUsesTheRequestedSubscribedEvent(t *testing.T) {
	service, store, deliverer := newTestRunService(t, subscribedTestHook())

	if _, err := service.SendTest(context.Background(), appmodel.WebhookTestRequest{
		TeamID: 7, CallerID: 11, WebhookID: 1, Event: "invoice.created",
	}); err != nil {
		t.Fatal(err)
	}
	if deliverer.request.Event != "invoice.created" {
		t.Fatalf("event = %q, want the requested subscribed event", deliverer.request.Event)
	}
	if _, err := service.SendTest(context.Background(), appmodel.WebhookTestRequest{
		TeamID: 7, CallerID: 11, WebhookID: 1, Event: "invoice.paid",
	}); !errors.Is(err, ErrInvalidWebhook) {
		t.Fatalf("event the endpoint is not subscribed to = %v, want an invalid webhook", err)
	}
	if _, err := service.SendTest(context.Background(), appmodel.WebhookTestRequest{
		TeamID: 7, CallerID: 11, WebhookID: 1, Event: "invoice.exploded",
	}); !errors.Is(err, ErrInvalidWebhook) {
		t.Fatalf("unknown event error = %v, want an invalid webhook", err)
	}
	if len(store.logs) != 1 {
		t.Fatalf("delivery log rows = %d, want only the delivered test run", len(store.logs))
	}
}

// A subscriber to every event still gets a real event name, not the wildcard:
// the receiver routes on the event field.
func TestTestRunOnWildcardEndpointSendsARealEventName(t *testing.T) {
	hook := subscribedTestHook()
	hook.Events = "*"
	service, _, deliverer := newTestRunService(t, hook)

	if _, err := service.SendTest(context.Background(), appmodel.WebhookTestRequest{TeamID: 7, CallerID: 11, WebhookID: 1}); err != nil {
		t.Fatal(err)
	}
	if deliverer.request.Event != string(defaultTestEvent) {
		t.Fatalf("event = %q, want %q", deliverer.request.Event, defaultTestEvent)
	}
}

// The receiver's answer is what makes the screen useful, and the failure must
// stay a safe category rather than an adapter message with the URL in it.
func TestTestRunReportsDeliveryFailureWithoutAdapterDetails(t *testing.T) {
	service, store, _ := newTestRunService(t, subscribedTestHook())
	service.deliverer = failingDeliverer{err: errors.New("dial https://hooks.example.test/events?token=top-secret: refused")}

	result, err := service.SendTest(context.Background(), appmodel.WebhookTestRequest{TeamID: 7, CallerID: 11, WebhookID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Error != "delivery failed" || strings.Contains(result.Error, "example.test") {
		t.Fatalf("error = %q, want a safe delivery category", result.Error)
	}
	if len(store.logs) != 1 || store.logs[0].Error != "delivery failed" {
		t.Fatalf("delivery log = %+v, want the failed attempt recorded once", store.logs)
	}
}

// A member must not be able to make the application POST on their behalf.
func TestTestRunStopsWhenTheEndpointCannotBeLoaded(t *testing.T) {
	service, store, deliverer := newTestRunService(t, subscribedTestHook())
	store.err = errors.New("forbidden")

	if _, err := service.SendTest(context.Background(), appmodel.WebhookTestRequest{TeamID: 7, CallerID: 11, WebhookID: 1}); err == nil {
		t.Fatal("a rejected endpoint lookup still delivered")
	}
	if len(store.logs) != 0 || deliverer.request.URL != "" {
		t.Fatalf("a rejected test run logged %d attempts and reached %q", len(store.logs), deliverer.request.URL)
	}
}

func TestTestRunRejectsMissingScope(t *testing.T) {
	service, _, _ := newTestRunService(t, subscribedTestHook())
	for _, request := range []appmodel.WebhookTestRequest{
		{TeamID: 0, CallerID: 11, WebhookID: 1},
		{TeamID: 7, CallerID: 0, WebhookID: 1},
		{TeamID: 7, CallerID: 11, WebhookID: 0},
	} {
		if _, err := service.SendTest(context.Background(), request); !errors.Is(err, ErrInvalidWebhook) {
			t.Fatalf("SendTest(%+v) = %v, want an invalid webhook", request, err)
		}
	}
}

type failingDeliverer struct{ err error }

func (d failingDeliverer) Deliver(context.Context, DeliveryRequest) (int, error) {
	return 0, d.err
}
