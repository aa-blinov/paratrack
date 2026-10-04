package appmodel

import (
	"time"

	"github.com/aa-blinov/paratrack/internal/mailport"
	"github.com/aa-blinov/paratrack/internal/webhookport"
)

type InvoiceEmailRetryRequest struct {
	Job         mailport.InvoiceEmailJob
	Message     string
	AvailableAt time.Time
	MaxAttempts int
}

type WebhookDeliveryRetryRequest struct {
	Job         webhookport.DeliveryJob
	AvailableAt time.Time
	MaxAttempts int
}

type WebhookEventRetryRequest struct {
	Event       webhookport.CommittedEvent
	AvailableAt time.Time
}
