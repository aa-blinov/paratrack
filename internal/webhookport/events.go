package webhookport

import (
	"encoding/json"
	"errors"
	"strings"
)

// Event is one supported outbound webhook contract. Its exported fields are
// encoded as the JSON value in the envelope's data field.
type Event interface {
	EventName() EventName
	webhookEvent()
}

// DecodeEvent decodes the versioned-by-type payload stored in the event
// outbox. Unknown names fail instead of being delivered with an untyped body.
func DecodeEvent(name EventName, payload []byte) (Event, error) {
	var event Event
	switch name {
	case EventSessionStarted:
		event = &SessionStartedEvent{}
	case EventSessionStopped:
		event = &SessionStoppedEvent{}
	case EventInvoiceCreated:
		event = &InvoiceCreatedEvent{}
	case EventInvoicePaid:
		event = &InvoicePaidEvent{}
	case EventPaymentLinkCreated:
		event = &PaymentLinkCreatedEvent{}
	case EventImportCompleted:
		event = &ImportCompletedEvent{}
	default:
		return nil, ErrUnknownEvent
	}
	if err := json.Unmarshal(payload, event); err != nil {
		return nil, err
	}
	return event, nil
}

// Subscribes reports whether an endpoint's stored filter includes an event.
func Subscribes(events string, event EventName) bool {
	for _, candidate := range strings.Split(events, ",") {
		if value := strings.TrimSpace(candidate); value == string(event) || value == "*" {
			return true
		}
	}
	return false
}

// ErrUnknownEvent is returned when a stored event name has no registered type.
var ErrUnknownEvent = errors.New("unknown webhook event")

// EventName identifies an externally visible webhook event.
type EventName string

// Event names are part of the externally visible webhook contract. Keep
// producers and endpoint validation on these shared values so a publisher
// cannot silently drift from the names clients subscribe to.
const (
	EventSessionStarted     EventName = "session.started"
	EventSessionStopped     EventName = "session.stopped"
	EventInvoiceCreated     EventName = "invoice.created"
	EventInvoicePaid        EventName = "invoice.paid"
	EventPaymentLinkCreated EventName = "invoice.payment_link_created"
	EventImportCompleted    EventName = "import.completed"
)

// SessionStartedEvent is published after a timer starts or focus creates one.
type SessionStartedEvent struct {
	SessionID int64  `json:"session_id"`
	Activity  string `json:"activity"`
}

func (SessionStartedEvent) EventName() EventName { return EventSessionStarted }
func (SessionStartedEvent) webhookEvent()        {}

// SessionStoppedEvent is published after a timer stops.
type SessionStoppedEvent struct {
	SessionID  int64  `json:"session_id"`
	ActivityID int64  `json:"activity_id"`
	Start      string `json:"start"`
}

func (SessionStoppedEvent) EventName() EventName { return EventSessionStopped }
func (SessionStoppedEvent) webhookEvent()        {}

// InvoiceCreatedEvent is published after a draft invoice is created.
type InvoiceCreatedEvent struct {
	InvoiceID int64  `json:"invoice_id"`
	Number    string `json:"number"`
	Client    string `json:"client"`
}

func (InvoiceCreatedEvent) EventName() EventName { return EventInvoiceCreated }
func (InvoiceCreatedEvent) webhookEvent()        {}

// InvoicePaidEvent is published after an invoice transitions to paid.
type InvoicePaidEvent struct {
	InvoiceID int64 `json:"invoice_id"`
	TeamID    int64 `json:"team_id"`
}

func (InvoicePaidEvent) EventName() EventName { return EventInvoicePaid }
func (InvoicePaidEvent) webhookEvent()        {}

// PaymentLinkCreatedEvent is published after a payment link is stored.
type PaymentLinkCreatedEvent struct {
	InvoiceID  int64  `json:"invoice_id"`
	Number     string `json:"number"`
	PaymentURL string `json:"payment_url"`
}

func (PaymentLinkCreatedEvent) EventName() EventName { return EventPaymentLinkCreated }
func (PaymentLinkCreatedEvent) webhookEvent()        {}

// ImportCompletedEvent is published after an import batch is committed.
type ImportCompletedEvent struct {
	Provider string `json:"provider"`
	Imported int    `json:"imported"`
}

func (ImportCompletedEvent) EventName() EventName { return EventImportCompleted }
func (ImportCompletedEvent) webhookEvent()        {}
