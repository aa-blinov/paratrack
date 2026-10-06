package webhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/webhookport"
)

// testDeliveryTimeout keeps the settings page responsive: the outbound client
// has its own shorter limit, so a silent endpoint answers before this one does.
const testDeliveryTimeout = 15 * time.Second

// defaultTestEvent is the sample used when an endpoint subscribes to every
// event. Session stopped is what the settings form preselects.
const defaultTestEvent = webhookport.EventSessionStopped

// responseDeliverer is the optional richer delivery port. Adapters that can
// return what the receiver answered let the settings screen show the response
// body; the plain port keeps delivering without one.
type responseDeliverer interface {
	DeliverWithResponse(context.Context, DeliveryRequest) (int, string, error)
}

// SendTest delivers one synthetic event to an endpoint on demand. It builds the
// same envelope, HMAC signature and transport as a queued delivery, so the
// receiver verifies exactly what production sends, and returns what came back
// so the screen can show the result without a detour into the history.
func (s *Service) SendTest(ctx context.Context, request appmodel.WebhookTestRequest) (appmodel.WebhookTestResult, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.WebhookID <= 0 {
		return appmodel.WebhookTestResult{}, fmt.Errorf("%w: team, caller and endpoint IDs must be positive", ErrInvalidWebhook)
	}
	teamID, callerID := request.TeamID, request.CallerID
	hook, err := s.store.GetWebhookForDelivery(ctx, appmodel.WebhookLookupCommand{
		TeamID: teamID, CallerID: callerID, WebhookID: request.WebhookID,
	})
	if err != nil {
		return appmodel.WebhookTestResult{}, err
	}
	event, err := testEvent(hook.Events, request.Event)
	if err != nil {
		return appmodel.WebhookTestResult{}, err
	}
	sentAt := s.now().UTC()
	body, err := testEnvelope(event, teamID, sentAt)
	if err != nil {
		return appmodel.WebhookTestResult{}, err
	}
	deliveryCtx, cancel := context.WithTimeout(ctx, testDeliveryTimeout)
	defer cancel()
	status, responseBody, deliverErr := s.deliverAttempt(deliveryCtx, hook, string(event), body)
	result := appmodel.WebhookTestResult{
		Event: string(event), Status: status, SentAt: sentAt,
		RequestBody: string(body), ResponseBody: responseBody,
	}
	if deliverErr != nil {
		result.Error = deliveryFailureMessage(deliverErr)
	}
	s.recordAudit(ctx, teamID, callerID, "webhook.test", fmt.Sprint(hook.ID), string(event))
	return result, nil
}

// testEvent picks the event the sample carries: the requested one when the
// endpoint subscribes to it, otherwise the first subscribed event. The sample
// therefore mirrors a delivery this endpoint would really receive, and a caller
// that asks for something else is told instead of silently sent another event.
func testEvent(subscriptions, requested string) (webhookport.EventName, error) {
	if chosen := webhookport.EventName(strings.TrimSpace(requested)); chosen != "" {
		if _, known := knownEvents[chosen]; !known {
			return "", fmt.Errorf("%w: unsupported event %q", ErrInvalidWebhook, chosen)
		}
		if !webhookport.Subscribes(subscriptions, chosen) {
			return "", fmt.Errorf("%w: endpoint is not subscribed to %q", ErrInvalidWebhook, chosen)
		}
		return chosen, nil
	}
	for _, candidate := range strings.Split(subscriptions, ",") {
		name := webhookport.EventName(strings.TrimSpace(candidate))
		if name == "*" {
			return defaultTestEvent, nil
		}
		if _, known := knownEvents[name]; known {
			return name, nil
		}
	}
	return defaultTestEvent, nil
}

// testEnvelope builds the production envelope and marks it as a manual test.
// Receivers read the marker from the body and never see it on live events,
// because the field is omitted there.
func testEnvelope(event webhookport.EventName, teamID int64, sentAt time.Time) ([]byte, error) {
	data, err := json.Marshal(sampleEvent(event, sentAt))
	if err != nil {
		return nil, fmt.Errorf("encode webhook test data: %w", err)
	}
	body, err := json.Marshal(deliveryPayload{
		Event: event, TeamID: teamID,
		SentAt: sentAt.Format(time.RFC3339), Action: "test", Data: json.RawMessage(data),
	})
	if err != nil {
		return nil, fmt.Errorf("encode webhook test event: %w", err)
	}
	return body, nil
}

// sampleEvent keeps the sample shaped like the real event, so an integrator's
// decoder sees the fields it expects. Identifiers stay zero: together with the
// envelope's action they mark the delivery as synthetic instead of pointing at
// a real record.
func sampleEvent(event webhookport.EventName, sentAt time.Time) any {
	switch event {
	case webhookport.EventSessionStarted:
		return webhookport.SessionStartedEvent{Activity: "test"}
	case webhookport.EventSessionStopped:
		return webhookport.SessionStoppedEvent{Start: sentAt.Format(time.RFC3339)}
	case webhookport.EventInvoiceCreated:
		return webhookport.InvoiceCreatedEvent{Number: "test", Client: "test"}
	case webhookport.EventInvoicePaid:
		return webhookport.InvoicePaidEvent{}
	case webhookport.EventPaymentLinkCreated:
		return webhookport.PaymentLinkCreatedEvent{Number: "test"}
	case webhookport.EventImportCompleted:
		return webhookport.ImportCompletedEvent{Provider: "test"}
	default:
		return defaultTestEvent
	}
}
