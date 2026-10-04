package appmodel

import "time"

type WebhookManagementSnapshot struct {
	Endpoints  []WebhookEndpointView
	Deliveries map[int64][]WebhookDeliveryView
}

type WebhookEndpointView struct {
	ID     int64
	URL    string
	Events string
	Active bool
}

type WebhookDeliveryView struct {
	CreatedAt time.Time
	Event     string
	Status    int
	Error     string
}

// TeamMemberManagementSnapshot contains the member directory and payroll
