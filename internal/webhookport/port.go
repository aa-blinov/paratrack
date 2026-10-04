// Package webhookport defines the outbound contract for webhook delivery.
package webhookport

import (
	"context"
	"errors"
)

// ErrPrivateTarget reports that outbound network policy blocked a target.
var ErrPrivateTarget = errors.New("webhook target blocked by network policy")

// DeliveryRequest is the signed, protocol-ready webhook message.
type DeliveryRequest struct {
	URL         string
	Event       string
	Timestamp   string
	Signature   string
	SignatureV2 string
	Body        []byte
}

// Deliverer sends one signed webhook request and returns the HTTP status.
type Deliverer interface {
	Deliver(context.Context, DeliveryRequest) (int, error)
}
