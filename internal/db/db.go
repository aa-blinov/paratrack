// Package db manages the paratrack database (Postgres).
//
// Timestamps are stored as RFC3339Nano UTC strings (e.g. "2026-09-22T07:25:30.123456Z").
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx"
)

// ErrNilDatabaseContext is returned when startup is called without a lifecycle
// context. Opening the database always performs cancellable migrations.
var ErrNilDatabaseContext = errors.New("database startup context is nil")

var ErrMissingLogger = errors.New("database logger is required")
var ErrMissingClock = errors.New("database clock is required")

// DB wraps the connection and exposes typed queries.
type DB struct {
	sql     *Conn
	secrets secretCodec
	logger  *log.Logger
	now     func() time.Time
}

// Config contains connection, at-rest encryption, and logging settings
// supplied by the process composition root.
type Config struct {
	URL              string
	SecretKey        string
	RequireSecretKey bool
	Logger           *log.Logger
	Now              func() time.Time
}

// OpenConfiguredContext connects to Postgres and applies schema and data
// migrations using explicit process configuration and cancellable startup.
func OpenConfiguredContext(ctx context.Context, config Config) (*DB, error) {
	if ctx == nil {
		return nil, ErrNilDatabaseContext
	}
	if config.RequireSecretKey && strings.TrimSpace(config.SecretKey) == "" {
		return nil, errors.New("PARATRACK_SECRET_KEY is required in this environment")
	}
	if config.URL == "" {
		return nil, errors.New("PARATRACK_DATABASE_URL is not set (postgres://user:pass@host:5432/db)")
	}
	if config.Logger == nil {
		return nil, ErrMissingLogger
	}
	if config.Now == nil {
		return nil, ErrMissingClock
	}
	return openContext(ctx, config.URL, config.SecretKey, config.Logger, config.Now)
}

func openContext(ctx context.Context, url, secretKey string, logger *log.Logger, now func() time.Time) (*DB, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sdb, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	sdb.SetMaxOpenConns(10)
	sdb.SetConnMaxIdleTime(5 * time.Minute)
	d := &DB{
		sql:     &Conn{db: sdb},
		secrets: newSecretCodec(secretKey),
		logger:  logger,
		now:     now,
	}
	steps := []struct {
		name string
		fn   func() error
	}{
		{"apply schema", func() error { return d.applySchemaContext(ctx) }},
		{"activity keys", func() error { return d.migrateActivityKeys(ctx) }},
		{"seal secrets", func() error { return d.sealExistingSecretsContext(ctx) }},
		{"hash invite tokens", func() error { return d.migrateInviteTokensContext(ctx) }},
		{"assign orphan sessions", func() error { return d.assignOrphanSessions(ctx) }},
		{"stamp legacy invoices", func() error { return d.stampLegacyInvoices(ctx) }},
	}
	closeAfterStartupFailure := func(primary error) error {
		if closeErr := sdb.Close(); closeErr != nil {
			return errors.Join(primary, fmt.Errorf("close database after startup failure: %w", closeErr))
		}
		return primary
	}
	for _, st := range steps {
		if err := ctx.Err(); err != nil {
			return nil, closeAfterStartupFailure(fmt.Errorf("%s: %w", st.name, err))
		}
		if err := st.fn(); err != nil {
			return nil, closeAfterStartupFailure(fmt.Errorf("%s: %w", st.name, err))
		}
	}
	return d, nil
}

func (d *DB) currentTime() time.Time { return d.now() }

// TestSQL exposes the connection for integration-test setup and diagnostics.
// Production code must use typed operations in this package. The architecture
// check restricts callers to test files.
func (d *DB) TestSQL() *Conn { return d.sql }

// Close releases the underlying database handle.
func (d *DB) Close() error { return d.sql.Close() }

// FormatTime returns the canonical RFC3339Nano UTC string used to store
// timestamps. Centralised so all writers agree.
func FormatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// ParseTime accepts the multiple timestamp formats we may encounter:
// RFC3339Nano (Go-native), and ISO 8601 with 'T' separator (Python legacy).
// Missing timezone is treated as UTC.
func ParseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05.999999",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised timestamp %q", s)
}

// ScanTime is a convenience wrapper for sql.Scan that handles NULL.
func ScanTime(src any) (time.Time, error) {
	switch v := src.(type) {
	case nil:
		return time.Time{}, nil
	case string:
		if v == "" {
			return time.Time{}, nil
		}
		return ParseTime(v)
	case []byte:
		if len(v) == 0 {
			return time.Time{}, nil
		}
		return ParseTime(string(v))
	case time.Time:
		return v.UTC(), nil
	default:
		return time.Time{}, fmt.Errorf("unsupported time source: %T", src)
	}
}

// isUniqueViolation returns true when err is a UNIQUE constraint
// violation. Used by get-or-create helpers that have to fall through
// to a SELECT after a race-condition INSERT.
func isUniqueViolation(err error) bool { return IsUniqueViolation(err) }

// IsUniqueViolation is isUniqueViolation for other packages.
func IsUniqueViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}
