package app

import (
	"errors"
	"log"
	"time"
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
