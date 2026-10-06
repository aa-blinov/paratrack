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
	ID            int64
	CreatedAt     time.Time
	Event         string
	Status        int
	Error         string
	RequestBody   string
	ResponseBody  string
	BodyTruncated bool
}

// WebhookTestResult is what the receiver answered to a manual test run.
type WebhookTestResult struct {
	Event        string
	Status       int
	Error        string
	RequestBody  string
	ResponseBody string
	SentAt       time.Time
}

// TeamMemberManagementSnapshot contains the member directory and payroll
