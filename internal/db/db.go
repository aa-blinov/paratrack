// Package db manages the paratrack SQLite database.
//
// Timestamps are stored as RFC3339Nano UTC strings (e.g. "2026-09-22T07:25:30.123456Z").
// Python's tracker used local-time ISO 8601 without timezone; a future
// migration command will rewrite those values. Until then, opening a
// Python-created DB works for read/append but historical timestamps may
// appear in the local zone.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	// Pure-Go SQLite driver; no CGO required.
	_ "modernc.org/sqlite"
)

// DefaultPath is ~/.track/track.db — same location as the original Python
// implementation, so a user can swap tools without losing data.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".track")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "track.db"), nil
}

// DB wraps a *sql.DB and exposes typed queries.
type DB struct {
	sql *sql.DB
}

// Open opens (or creates) the SQLite database at path and applies the
// schema. The caller must Close when done.
func Open(path string) (*DB, error) {
	// _pragma options: foreign keys on, WAL for safer concurrent reads,
	// busy timeout so CLI + web don't deadlock when both touch the file.
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	sdb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite is happiest with a single writer; many readers. Cap conns.
	sdb.SetMaxOpenConns(1)
	sdb.SetMaxIdleConns(1)
	sdb.SetConnMaxLifetime(0)

	d := &DB{sql: sdb}
	if _, err := sdb.Exec(schema); err != nil {
		_ = sdb.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := d.applyMigrations(); err != nil {
		_ = sdb.Close()
		return nil, fmt.Errorf("apply migrations: %w", err)
	}
	return d, nil
}

// Close releases the underlying database handle.
func (d *DB) Close() error { return d.sql.Close() }

// SQL exposes the raw *sql.DB for advanced callers (migrations, tests).
// Production code should use the typed helpers in activities.go and sessions.go.
func (d *DB) SQL() *sql.DB { return d.sql }

// FormatTime returns the canonical RFC3339Nano UTC string used to store
// timestamps in SQLite. Centralised so all writers agree.
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
