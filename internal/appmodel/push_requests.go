package appmodel

type PushUnsubscribeRequest struct {
	TeamID   int64
	UserID   int64
	Endpoint string `json:"-"`
}
