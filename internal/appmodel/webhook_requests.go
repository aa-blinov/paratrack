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

// WebhookDeliveryLogRequest records one delivery attempt. Bodies are stored
// truncated so a chatty receiver cannot grow the history without bound, and the
// signing secret is never part of them: it only travels in a header.
type WebhookDeliveryLogRequest struct {
	WebhookID    int64
	Event        string
	Status       int
	Error        string
	RequestBody  string `json:"-"`
	ResponseBody string `json:"-"`
}

// WebhookLookupCommand resolves one endpoint for an outbound attempt and
// re-checks the caller's role, because the answer carries the signing secret.
type WebhookLookupCommand struct {
	TeamID    int64
	CallerID  int64
	WebhookID int64
}

// WebhookTestRequest asks for one synthetic delivery to an endpoint on demand.
type WebhookTestRequest struct {
	TeamID    int64
	CallerID  int64
	WebhookID int64
	Event     string
}

type WebhookDeleteRequest struct {
	TeamID    int64
	CallerID  int64
	WebhookID int64
}
