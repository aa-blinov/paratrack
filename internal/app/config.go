package app

import (
	"errors"
	"log"
	"time"

	"github.com/aa-blinov/paratrack/internal/mail"
)

var (
	ErrMissingLogger = errors.New("application logger is required")
	ErrMissingClock  = errors.New("application clock is required")
)

// Config contains process settings required while composing application services.
type Config struct {
	Logger                  *log.Logger
	WebhookAllowPrivate     bool
	IntegrationAllowPrivate bool
	StripeAPIKey            string
	StripeWebhookSecret     string
	JiraSite                string
	GitLabSite              string
	DefaultTimezone         string
	Now                     func() time.Time
	// SMTP and PublicURL let reminders reach managers by email (TIME-14).
	SMTP      mail.SMTPConfig
	PublicURL string
	// CalendarGoogle and CalendarMicrosoft are the OAuth clients of calendar sync (CRM-19).
	// A provider with an empty client is not offered; with neither, the sync is off.
	CalendarGoogle    CalendarOAuthClient
	CalendarMicrosoft CalendarOAuthClient
}

// CalendarOAuthClient is one calendar provider's OAuth client, as the process config reads it.
type CalendarOAuthClient struct {
	ClientID     string
	ClientSecret string
}

// CLIConfig contains only the process dependency required by CLI workflows.
// HTTP-only providers, payment credentials and clocks stay out of this graph.
type CLIConfig struct {
	Logger *log.Logger
}

// validateConfig ensures both transport graphs receive the process-owned
// logger and clock instead of selecting independent package defaults.
func validateConfig(config Config) error {
	if config.Logger == nil {
		return ErrMissingLogger
	}
	if config.Now == nil {
		return ErrMissingClock
	}
	return nil
}

func validateCLIConfig(config CLIConfig) error {
	if config.Logger == nil {
		return ErrMissingLogger
	}
	return nil
}
