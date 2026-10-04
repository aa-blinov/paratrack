package appmodel

// WebhookLookupQuery resolves one endpoint within its workspace.
type WebhookLookupQuery struct {
	TeamID    int64
	WebhookID int64
}

// WebhookManagementQuery scopes the settings snapshot and bounds each endpoint's history.
type WebhookManagementQuery struct {
	TeamID                int64
	DeliveriesPerEndpoint int
}

// WebhookListQuery scopes endpoint reads to one workspace.
type WebhookListQuery struct {
	TeamID int64
}

// WebhookDeliveryHistoryQuery scopes recent delivery reads and sets their per-endpoint bound.
type WebhookDeliveryHistoryQuery struct {
	TeamID int64
	Limit  int
}

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
