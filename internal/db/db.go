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
	"strings"
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
//
// If a pre-auth (Phase-0) database is found at path — i.e. the file
// exists but contains no `users` table — the file is archived with a
// timestamped `.bak` suffix and a fresh schema is created. This is the
// hard break the product team chose: collaboration can't be added on
// top of the single-user schema, so old rows go into the archive.
func Open(path string) (*DB, error) {
	if err := archiveIfLegacy(path); err != nil {
		return nil, fmt.Errorf("archive legacy DB: %w", err)
	}

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

// archiveIfLegacy detects a Phase-0 single-user database and renames
// it out of the way before the new schema is created. Returns nil for
// fresh installs and for databases that already carry the new schema.
//
// Detection rule: the file must exist, AND opening it in read-only mode
// must NOT contain a `users` table. The presence of the file is what
// distinguishes "legacy to be archived" from "fresh install, create new".
func archiveIfLegacy(path string) error {
	if _, err := os.Stat(path); err != nil {
		// No file — nothing to archive.
		return nil
	}
	hasUsers, err := hasTable(path, "users")
	if err != nil {
		return err
	}
	if hasUsers {
		// Already on the new schema.
		return nil
	}

	stamp := time.Now().UTC().Format("20060102-150405")
	base := strings.TrimSuffix(path, filepath.Ext(path))
	backup := base + ".bak." + stamp
	if err := os.Rename(path, backup); err != nil {
		return err
	}
	// WAL/SHM siblings are not committed to disk on close, but if the
	// previous process died mid-write they may still be around. Move
	// them out of the way too so the next Open() doesn't pick them up.
	for _, ext := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(path + ext); err == nil {
			_ = os.Rename(path+ext, backup+ext)
		}
	}
	return nil
}

// hasTable opens a read-only SQLite connection at path and reports
// whether the named table exists. The connection is closed before
// returning so no handles linger on the file.
func hasTable(path, table string) (bool, error) {
	dsn := fmt.Sprintf("file:%s?mode=ro", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return false, err
	}
	defer db.Close()
	var name string
	row := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
	)
	if err := row.Scan(&name); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return name == table, nil
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

// isUniqueViolation returns true when err is an SQLite UNIQUE constraint
// violation. Used by get-or-create helpers that have to fall through to
// a SELECT after a race-condition INSERT.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, "UNIQUE constraint") ||
		strings.Contains(msg, "constraint failed: UNIQUE") {
		return true
	}
	type coder interface{ Code() int }
	if c, ok := err.(coder); ok && c.Code() == 2067 {
		return true
	}
	return false
}