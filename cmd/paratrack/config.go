package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
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
	return makeDatabaseConfig(environment, os.Getenv("PARATRACK_DATABASE_URL"), os.Getenv("PARATRACK_SECRET_KEY"), logger, now, os.Getenv("PARATRACK_DB_MAX_CONNS"))
}

// parseDBMaxConns reads the pool override. An unset or invalid value keeps
// the database package's default rather than failing startup: a mistyped
// number should not take a self-hosted instance down.
func parseDBMaxConns(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

func makeDatabaseConfig(environment, databaseURL, secretKey string, logger *log.Logger, now func() time.Time, rawMaxConns string) db.Config {
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
		MaxOpenConns:     parseDBMaxConns(rawMaxConns),
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
	googleCalendar, err := calendarCredentialsFrom(getenv, "PARATRACK_GOOGLE_CALENDAR")
	if err != nil {
		return processConfig{}, err
	}
	microsoftCalendar, err := calendarCredentialsFrom(getenv, "PARATRACK_MICROSOFT_CALENDAR")
	if err != nil {
		return processConfig{}, err
	}
	now := time.Now
	logger := log.Default()
	smtp := mail.SMTPConfig{
		Host: getenv("PARATRACK_SMTP_HOST"), Username: getenv("PARATRACK_SMTP_USER"),
		Password: getenv("PARATRACK_SMTP_PASS"), From: getenv("PARATRACK_MAIL_FROM"),
		// Off unless set to 1: a relay that offers no STARTTLS refuses to
		// carry the reset links. A local relay beside the binary opts in.
		AllowPlaintext: getenv("PARATRACK_SMTP_ALLOW_PLAINTEXT") == "1",
	}
	return processConfig{
		database: makeDatabaseConfig(environment, getenv("PARATRACK_DATABASE_URL"), getenv("PARATRACK_SECRET_KEY"), logger, now, getenv("PARATRACK_DB_MAX_CONNS")),
		services: app.Config{
			Logger:                  logger,
			Now:                     now,
			WebhookAllowPrivate:     getenv("PARATRACK_WEBHOOK_ALLOW_PRIVATE") == "1",
			IntegrationAllowPrivate: getenv("PARATRACK_INTEGRATION_ALLOW_PRIVATE") == "1",
			// On unless set to 0: a new account confirms its address before it can sign in.
			RequireEmailVerification: getenv("PARATRACK_REQUIRE_EMAIL_VERIFICATION") != "0",
			StripeAPIKey:             strings.TrimSpace(getenv("PARATRACK_STRIPE_KEY")),
			StripeWebhookSecret:      strings.TrimSpace(getenv("PARATRACK_STRIPE_WEBHOOK_SECRET")),
			JiraSite:                 getenv("PARATRACK_JIRA_SITE"),
			GitLabSite:               getenv("PARATRACK_GITLAB_SITE"),
			DefaultTimezone:          defaultTimezone,
			SMTP:                     smtp,
			PublicURL:                publicURL,
			CalendarGoogle:           googleCalendar,
			CalendarMicrosoft:        microsoftCalendar,
		},
		mailer: smtp,
		web: web.Config{
			Logger:          logger,
			AppVersion:      Version,
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

// calendarCredentialsFrom reads one provider's OAuth client for calendar sync. The
// client ID and secret are both or neither: half a client cannot complete a consent.
func calendarCredentialsFrom(getenv func(string) string, prefix string) (app.CalendarOAuthClient, error) {
	client := app.CalendarOAuthClient{
		ClientID:     strings.TrimSpace(getenv(prefix + "_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(getenv(prefix + "_CLIENT_SECRET")),
	}
	if (client.ClientID == "") != (client.ClientSecret == "") {
		return app.CalendarOAuthClient{}, fmt.Errorf("%s_CLIENT_ID and %s_CLIENT_SECRET must be configured together", prefix, prefix)
	}
	return client, nil
}

type cliRemoteFileConfig struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

func defaultCLIRemoteConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(dir) == "" {
		return ""
	}
	return filepath.Join(dir, "paratrack", "config.json")
}

func defaultCLIStatusCachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(dir) == "" {
		return ""
	}
	return filepath.Join(dir, "paratrack", "status.json")
}

func readCLIRemoteFile(path string) (string, string) {
	if strings.TrimSpace(path) == "" {
		return "", ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	var cfg cliRemoteFileConfig
	if json.Unmarshal(data, &cfg) == nil {
		return strings.TrimSpace(cfg.URL), strings.TrimSpace(cfg.Token)
	}
	return "", ""
}

func loadCLIRemoteConfig(configPath string) (string, string) {
	u := strings.TrimSpace(os.Getenv("PARATRACK_URL"))
	tok := strings.TrimSpace(os.Getenv("PARATRACK_TOKEN"))
	if u != "" && tok != "" {
		return u, tok
	}
	for _, candidate := range []string{".paratrack", configPath} {
		fu, ftok := readCLIRemoteFile(candidate)
		if u == "" {
			u = fu
		}
		if tok == "" {
			tok = ftok
		}
		if u != "" && tok != "" {
			break
		}
	}
	return u, tok
}
