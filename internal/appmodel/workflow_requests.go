package appmodel

type WebhookCreateRequest struct {
	TeamID   int64
	CallerID int64
	URL      string `json:"-"`
	Secret   string `json:"-"` // Sensitive signing material; never include in audit metadata.
	Events   []string
}

type PushSubscribeRequest struct {
	TeamID     int64
	UserID     int64
	CallerID   int64
	Endpoint   string `json:"-"`
	PublicKey  string `json:"-"`
	AuthSecret string `json:"-"` // Sensitive subscription material.
}
