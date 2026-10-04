package webhookport

import "time"

// DeliveryJob is a leased endpoint delivery from the persistent queue.
// Credentials and payload are limited to the delivery workflow and adapter.
type DeliveryJob struct {
	ID        int64
	TeamID    int64
	WebhookID int64
	URL       string `json:"-"`
	Secret    string `json:"-"`
	Event     string
	Payload   []byte `json:"-"`
	Attempts  int
	Lease     string `json:"-"`
}

// CommittedEvent is an application event claimed from the transactional outbox.
type CommittedEvent struct {
	ID         int64
	TeamID     int64
	Event      string
	Payload    []byte `json:"-"`
	WebhookIDs []int64
	Attempts   int
	Lease      string `json:"-"`
	CreatedAt  time.Time
}

// EventRetryRequest reschedules a leased outbox event after fan-out fails.
type EventRetryRequest struct {
	Event       CommittedEvent
	AvailableAt time.Time
}

// DeliveryRetryRequest reschedules or expires a leased endpoint delivery.
type DeliveryRetryRequest struct {
	Job         DeliveryJob
	AvailableAt time.Time
	MaxAttempts int
}

// Webhook is an endpoint loaded for delivery. Its signing secret must stay in
// the workflow and outbound delivery path.
type Webhook struct {
	ID        int64
	TeamID    int64
	URL       string
	Secret    string `json:"-"`
	Events    string
	Active    bool
	CreatedAt time.Time
}

// WebhookSummary is the credential-free endpoint data shown to managers.
type WebhookSummary struct {
	ID        int64
	TeamID    int64
	URL       string
	Events    string
	Active    bool
	CreatedAt time.Time
}

// WebhookDeliverySummary contains safe operational history for settings UI.
type WebhookDeliverySummary struct {
	ID        int64
	WebhookID int64
	Event     string
	Status    int
	Error     string
	CreatedAt time.Time
}
