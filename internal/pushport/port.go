// Package pushport defines the outbound contract for Web Push delivery.
package pushport

import (
	"context"
)

// Subscription is the credential-bearing endpoint used only for delivery.
type Subscription struct {
	ID       int64
	TeamID   int64
	UserID   int64
	Endpoint string `json:"-"`
	P256DH   string `json:"-"`
	Auth     string `json:"-"`
}

// Sender delivers a notification to a browser subscription.
type Sender interface {
	Send(context.Context, Subscription, string, string, []byte) (SendResult, error)
}

// SendResult reports provider outcomes that affect subscription lifecycle.
type SendResult struct {
	SubscriptionExpired bool
}
