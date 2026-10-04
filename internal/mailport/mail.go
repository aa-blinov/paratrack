// Package mailport defines the transport-neutral contract used by mail
// producers and delivery adapters.
package mailport

import (
	"context"

	"github.com/aa-blinov/paratrack/internal/depcheck"
)

// Sender delivers a single plain-text message. Implementations must be safe
// for concurrent use and should stop promptly when the context is canceled.
type Sender interface {
	Send(context.Context, string, string, string) error
}

// Attachment is a file sent along with a message.
type Attachment struct {
	Name        string
	ContentType string
	Data        []byte
}

// Message is a full letter with plain text, optional HTML, and optional files.
type Message struct {
	To, Subject, Text, HTML string
	Files                   []Attachment
}

// InvoiceQueueRequest freezes a prepared invoice email for durable delivery.
// It is a producer contract; delivery workers receive only Message.
type InvoiceQueueRequest struct {
	TeamID        int64
	InvoiceID     int64
	Revision      int64
	InvoiceNumber string
	Recipient     string
	Message       Message
}

// RichSender delivers a message with HTML and attachments, honoring context
// cancellation so application shutdown can bound in-flight delivery.
type RichSender interface {
	Deliver(context.Context, Message) error
}

// Available reports whether a sender is configured to deliver real messages.
func Available(sender Sender) bool {
	if depcheck.IsNil(sender) {
		return false
	}
	if readiness, ok := sender.(interface{ Available() bool }); ok {
		return readiness.Available()
	}
	return true
}
