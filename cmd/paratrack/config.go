package main

import (
	"fmt"
	"log"
	"math"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/app"
	"github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/mail"
	"github.com/aa-blinov/paratrack/internal/oidcclient"
	"github.com/aa-blinov/paratrack/internal/web"
)

type processConfig struct {
	database db.Config
	services app.Config
	web      web.Config
	oidc     oidcclient.Config
	mailer   mail.SMTPConfig
}

func loadDatabaseConfig(logger *log.Logger, now func() time.Time) db.Config {
	environment := strings.ToLower(strings.TrimSpace(os.Getenv("PARATRACK_ENV")))
	return makeDatabaseConfig(environment, os.Getenv("PARATRACK_DATABASE_URL"), os.Getenv("PARATRACK_SECRET_KEY"), logger, now)
}

func makeDatabaseConfig(environment, databaseURL, secretKey string, logger *log.Logger, now func() time.Time) db.Config {
	if now == nil {
		now = time.Now
	}
	if environment == "" {
		environment = "production"
	}
	return db.Config{
		Logger:           logger,
		URL:              databaseURL,
		SecretKey:        strings.TrimSpace(secretKey),
		RequireSecretKey: environment != "development" && environment != "test",
		Now:              now,
	}
}

func loadCLIServiceConfig(logger *log.Logger) app.CLIConfig {
	return app.CLIConfig{Logger: logger}
}

func loadCLITimezone() (*time.Location, error) {
	return loadCLITimezoneWith(os.Getenv)
}

func loadCLITimezoneWith(getenv func(string) string) (*time.Location, error) {
	if getenv == nil {
		return nil, fmt.Errorf("process environment lookup is nil")
	}
	return parseProcessTimezone(getenv("PARATRACK_TZ"))
}

func parseProcessTimezone(raw string) (*time.Location, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return time.Local, nil
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("parse PARATRACK_TZ: %w", err)
	}
	return location, nil
}

func loadProcessConfigWith(getenv func(string) string) (processConfig, error) {
	return loadProcessConfigWithTimezone(getenv, nil)
}

func loadProcessConfigWithTimezone(getenv func(string) string, timezone *time.Location) (processConfig, error) {
	if getenv == nil {
		return processConfig{}, fmt.Errorf("process environment lookup is nil")
	}
	traceRate := 1.0
	if raw := getenv("PARATRACK_SENTRY_TRACES"); raw != "" {
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return processConfig{}, fmt.Errorf("parse PARATRACK_SENTRY_TRACES: %w", err)
		}
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			return processConfig{}, fmt.Errorf("PARATRACK_SENTRY_TRACES must be between 0 and 1")
		}
		traceRate = value
	}
	environment := strings.ToLower(strings.TrimSpace(getenv("PARATRACK_ENV")))
	publicURL := strings.TrimRight(strings.TrimSpace(getenv("PARATRACK_PUBLIC_URL")), "/")
	if timezone == nil {
		resolvedTimezone, err := parseProcessTimezone(getenv("PARATRACK_TZ"))
		if err != nil {
			return processConfig{}, err
		}
		timezone = resolvedTimezone
	}
	// Capture the zone once so workflows and adapters share the same process
	// default instead of consulting time.Local after startup.
	defaultTimezone := timezone.String()
	var trustedProxies []netip.Prefix
	for _, raw := range strings.Split(getenv("PARATRACK_TRUSTED_PROXIES"), ",") {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return processConfig{}, fmt.Errorf("parse PARATRACK_TRUSTED_PROXIES entry %q: %w", value, err)
		}
		if prefix.Bits() == 0 {
			return processConfig{}, fmt.Errorf("PARATRACK_TRUSTED_PROXIES entry %q must not trust every address", value)
		}
		trustedProxies = append(trustedProxies, prefix.Masked())
	}
	if environment == "" {
		environment = "production"
	}
	productionLike := environment != "development" && environment != "test"
	if publicURL == "" && productionLike {
		return processConfig{}, fmt.Errorf("PARATRACK_PUBLIC_URL is required outside development and test")
	}
	if publicURL != "" {
		parsed, err := url.Parse(publicURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return processConfig{}, fmt.Errorf("PARATRACK_PUBLIC_URL must be an absolute http(s) origin without credentials, path, query, or fragment")
		}
		if productionLike && parsed.Scheme != "https" {
			return processConfig{}, fmt.Errorf("PARATRACK_PUBLIC_URL must use https outside development and test")
		}
	}
	oidcIssuer := strings.TrimSpace(getenv("PARATRACK_OIDC_ISSUER"))
	oidcConfig := oidcclient.Config{
		Issuer: oidcIssuer, ClientID: strings.TrimSpace(getenv("PARATRACK_OIDC_CLIENT_ID")),
		ClientSecret: getenv("PARATRACK_OIDC_CLIENT_SECRET"),
		TrustEmail:   getenv("PARATRACK_OIDC_TRUST_EMAIL") == "1", RequireHTTPS: productionLike,
	}
	if oidcIssuer != "" {
		parsed, err := url.Parse(oidcIssuer)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return processConfig{}, fmt.Errorf("PARATRACK_OIDC_ISSUER must be an absolute http(s) URL without credentials, query, or fragment")
		}
		if productionLike && parsed.Scheme != "https" {
			return processConfig{}, fmt.Errorf("PARATRACK_OIDC_ISSUER must use https outside development and test")
		}
	}
	oidcConfigured := 0
	for _, value := range []string{oidcConfig.Issuer, oidcConfig.ClientID, oidcConfig.ClientSecret} {
		if value != "" {
			oidcConfigured++
		}
	}
	if oidcConfigured != 0 && oidcConfigured != 3 {
		return processConfig{}, fmt.Errorf("PARATRACK_OIDC_ISSUER, PARATRACK_OIDC_CLIENT_ID, and PARATRACK_OIDC_CLIENT_SECRET must be configured together")
	}
	now := time.Now
	logger := log.Default()
	return processConfig{
		database: makeDatabaseConfig(environment, getenv("PARATRACK_DATABASE_URL"), getenv("PARATRACK_SECRET_KEY"), logger, now),
		services: app.Config{
			Logger:                  logger,
			Now:                     now,
			WebhookAllowPrivate:     getenv("PARATRACK_WEBHOOK_ALLOW_PRIVATE") == "1",
			IntegrationAllowPrivate: getenv("PARATRACK_INTEGRATION_ALLOW_PRIVATE") == "1",
			StripeAPIKey:            strings.TrimSpace(getenv("PARATRACK_STRIPE_KEY")),
			StripeWebhookSecret:     strings.TrimSpace(getenv("PARATRACK_STRIPE_WEBHOOK_SECRET")),
			JiraSite:                getenv("PARATRACK_JIRA_SITE"),
			GitLabSite:              getenv("PARATRACK_GITLAB_SITE"),
			DefaultTimezone:         defaultTimezone,
		},
		mailer: mail.SMTPConfig{
			Host: getenv("PARATRACK_SMTP_HOST"), Username: getenv("PARATRACK_SMTP_USER"),
			Password: getenv("PARATRACK_SMTP_PASS"), From: getenv("PARATRACK_MAIL_FROM"),
		},
		web: web.Config{
			Logger:          logger,
			Now:             now,
			PublicURL:       publicURL,
			OIDCEnabled:     oidcConfig.Issuer != "" && oidcConfig.ClientID != "",
			DefaultTimezone: defaultTimezone,
			TrustedProxies:  trustedProxies,
			Sentry: web.SentryConfig{
				DSN:              strings.TrimSpace(getenv("PARATRACK_SENTRY_DSN")),
				Environment:      environment,
				TracesSampleRate: traceRate,
			},
		},
		oidc: oidcConfig,
	}, nil
}
