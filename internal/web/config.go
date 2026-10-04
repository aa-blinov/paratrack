package web

import (
	"context"
	"log"
	"net/netip"
	"time"

	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/mailport"
	"github.com/aa-blinov/paratrack/internal/oidcport"
)

// RuntimeDependencies are adapter infrastructure owned by the HTTP process.
// The process root constructs and closes the OIDC client.
type RuntimeDependencies struct {
	OIDC                  OIDCProvider
	Mailer                mailport.Sender
	MailQueueWorker       InvoiceMailWorker
	WebhookDeliveryWorker WebhookDeliveryWorker
}

type OIDCProvider = oidcport.Provider
type OIDCAuthorizationRequest = oidcport.AuthorizationRequest
type OIDCAuthenticationRequest = oidcport.AuthenticationRequest
type OIDCIdentity = oidcport.Identity

// InvoiceMailWorker is a process lifecycle capability used by the HTTP
// adapter. Queue mutation stays on the workflow-facing InvoiceMailQueue port.
type InvoiceMailWorker interface {
	Run(context.Context, func(context.Context, mailport.Message) error, time.Duration)
}

// WebhookDeliveryWorker polls persistent deliveries until its context ends.
type WebhookDeliveryWorker interface {
	Run(context.Context, time.Duration)
}

func (d RuntimeDependencies) Close() {
	if !depcheck.IsNil(d.OIDC) {
		d.OIDC.CloseIdleConnections()
	}
}

// Config contains process-wide HTTP adapter settings. Request handlers read
// this immutable snapshot instead of consulting environment variables.
type Config struct {
	Logger          *log.Logger
	Now             func() time.Time
	OIDCEnabled     bool
	PublicURL       string
	DefaultTimezone string
	TrustedProxies  []netip.Prefix
	Sentry          SentryConfig
}

// SentryConfig configures process-wide error reporting.
type SentryConfig struct {
	DSN              string
	Environment      string
	TracesSampleRate float64
}
