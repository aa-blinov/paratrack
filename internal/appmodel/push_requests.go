package appmodel

type PushUnsubscribeRequest struct {
	TeamID   int64
	UserID   int64
	CallerID int64
	Endpoint string `json:"-"`
}

// PushSubscriptionCleanupRequest is an internal maintenance command used to
// remove endpoints rejected by the push provider after delivery.
type PushSubscriptionCleanupRequest struct {
	TeamID   int64
	UserID   int64
	Endpoint string `json:"-"`
}
