package appmodel

type WebhookRegistrationCommand struct {
	TeamID   int64
	CallerID int64
	URL      string `json:"-"`
	Secret   string `json:"-"`
	Events   string
}

type WebhookDeliveryBatchRequest struct {
	SourceEventID int64
	TeamID        int64
	WebhookIDs    []int64
	Event         string
	Payload       []byte `json:"-"`
}

type WebhookDeliveryLogRequest struct {
	WebhookID int64
	Event     string
	Status    int
	Error     string
}

type WebhookDeleteRequest struct {
	TeamID    int64
	CallerID  int64
	WebhookID int64
}
