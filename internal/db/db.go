// Package db manages the paratrack database (Postgres).
//
// Timestamps are stored as RFC3339Nano UTC strings (e.g. "2026-09-22T07:25:30.123456Z").
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx"
)

// DB wraps the connection and exposes typed queries.
type DB struct {
	sql *Conn
}

// OpenDefault opens the database at PARATRACK_DATABASE_URL.
func OpenDefault() (*DB, error) {
	url := os.Getenv("PARATRACK_DATABASE_URL")
	if url == "" {
		return nil, errors.New("PARATRACK_DATABASE_URL is not set (postgres://user:pass@host:5432/db)")
	}
	return Open(url)
}

// Open connects to a Postgres url and brings the schema up to date.
func Open(url string) (*DB, error) {
	sdb, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	sdb.SetMaxOpenConns(10)
	sdb.SetConnMaxIdleTime(5 * time.Minute)
	d := &DB{sql: &Conn{DB: sdb}}
	steps := []struct {
		name string
		fn   func() error
	}{
		{"apply schema", d.applySchema},
		{"activity keys", d.fillActivityKeys},
		{"seal secrets", d.sealExistingSecrets},
		{"assign orphan sessions", func() error { return d.assignOrphanSessions(context.Background()) }},
		{"stamp legacy invoices", func() error { return d.stampLegacyInvoices(context.Background()) }},
	}
	for _, st := range steps {
		if err := st.fn(); err != nil {
			_ = sdb.Close()
			return nil, fmt.Errorf("%s: %w", st.name, err)
		}
	}
	return d, nil
}

// Close releases the underlying database handle.
func (d *DB) Close() error { return d.sql.Close() }

// SQL exposes the connection for callers outside this package.
// Production code should use the typed helpers in activities.go and sessions.go.
func (d *DB) SQL() *Conn { return d.sql }

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

// NullTime returns nil for zero-value times, otherwise a pointer to UTC.
func NullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return FormatTime(t)
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
