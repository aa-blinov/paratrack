package db

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestGetOrCreateActivity_CaseInsensitive(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()

	a, err := d.GetOrCreateActivity(ctx, 0, "work")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "work" {
		t.Errorf("expected normalised name 'work', got %q", a.Name)
	}

	// Different case → same id, canonical lowercase name.
	for _, variant := range []string{"WORK", "Work", "wOrK"} {
		got, err := d.GetOrCreateActivity(ctx, 0, variant)
		if err != nil {
			t.Errorf("GetOrCreateActivity(%q): %v", variant, err)
			continue
		}
		if got.ID != a.ID || got.Name != "work" {
			t.Errorf("variant %q: got id=%d name=%q, want id=%d name='work'",
				variant, got.ID, got.Name, a.ID)
		}
	}
}

func TestGetActivityByName_CaseInsensitive(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	a, err := d.GetOrCreateActivity(ctx, 0, "Reading")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "reading" {
		t.Errorf("expected normalised 'reading', got %q", a.Name)
	}
	b, err := d.GetActivityByName(ctx, 0, "READING")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID {
		t.Errorf("case-insensitive lookup should find same id, got %d vs %d", a.ID, b.ID)
	}
}

func TestCreateActivity_TrimsAndLowercases(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	a, err := d.CreateActivity(ctx, 0, "  Writing  ")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "writing" {
		t.Errorf("expected trimmed+lowercased 'writing', got %q", a.Name)
	}
}

// TestNormalizeActivityCase_MergesDuplicates exercises the one-shot
// data migration that runs when a pre-case-insensitive DB is opened:
// duplicate rows that collide under COLLATE NOCASE get merged into
// the lowest-id winner, with their sessions re-pointed at the winner.
//
// Uses a "legacy" schema (binary UNIQUE on the name column) so we
// can actually plant rows like "Work" + "WORK" — the current schema
// would reject them at insert time.
func TestNormalizeActivityCase_MergesDuplicates(t *testing.T) {
	d := openLegacySchemaDB(t)
	ctx := t.Context()

	winnerID, err := insertRawActivity(ctx, d, "Work")
	if err != nil {
		t.Fatal(err)
	}
	loserID, err := insertRawActivity(ctx, d, "WORK")
	if err != nil {
		t.Fatal(err)
	}
	if winnerID == loserID {
		t.Fatal("raw inserts unexpectedly collided on the same row")
	}
	keepID, err := insertRawActivity(ctx, d, "Reading")
	if err != nil {
		t.Fatal(err)
	}

	sWinner, err := d.CreateSession(ctx, 0, winnerID, time.Now(), "")
	if err != nil {
		t.Fatal(err)
	}
	sLoser, err := d.CreateSession(ctx, 0, loserID, time.Now(), "")
	if err != nil {
		t.Fatal(err)
	}
	sKeep, err := d.CreateSession(ctx, 0, keepID, time.Now(), "")
	if err != nil {
		t.Fatal(err)
	}

	if err := d.normalizeActivityCase(); err != nil {
		t.Fatalf("normalizeActivityCase: %v", err)
	}

	if _, err := d.GetActivity(ctx, loserID); err == nil {
		t.Errorf("loser activity id=%d should be deleted, still present", loserID)
	}
	gotWinner, err := d.GetActivity(ctx, winnerID)
	if err != nil {
		t.Fatalf("winner vanished: %v", err)
	}
	if gotWinner.Name != "work" {
		t.Errorf("winner name = %q, want %q", gotWinner.Name, "work")
	}

	for _, sid := range []int64{sWinner.ID, sLoser.ID} {
		s, err := d.GetSession(ctx, 0, sid)
		if err != nil {
			t.Fatalf("session %d lookup: %v", sid, err)
		}
		if s.ActivityID != winnerID {
			t.Errorf("session %d activity_id = %d, want %d", sid, s.ActivityID, winnerID)
		}
	}
	keepSess, err := d.GetSession(ctx, 0, sKeep.ID)
	if err != nil {
		t.Fatal(err)
	}
	if keepSess.ActivityID != keepID {
		t.Errorf("untouched session activity_id = %d, want %d", keepSess.ActivityID, keepID)
	}

	// Idempotent — second run is a no-op.
	if err := d.normalizeActivityCase(); err != nil {
		t.Fatalf("second normalizeActivityCase: %v", err)
	}
	acts, err := d.ListActivities(ctx, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 2 {
		t.Errorf("after merge expected 2 activities, got %d: %+v", len(acts), acts)
	}
}

// insertRawActivity plants an activity row with whatever casing the
// caller specifies. The current schema enforces COLLATE NOCASE on the
// name column, so this helper only works against the legacy schema
// produced by openLegacySchemaDB. Used by migration tests to mimic
// the data state a real pre-upgrade DB would carry.
func insertRawActivity(ctx context.Context, d *DB, name string) (int64, error) {
	var id int64
	err := d.sql.QueryRowContext(ctx,
		`INSERT INTO activities (name, created_at, updated_at) VALUES (?, ?, ?) RETURNING id`,
		name, FormatTime(time.Now().UTC()), FormatTime(time.Now().UTC()),
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// openLegacySchemaDB creates an in-memory SQLite with the original
// (binary-UNIQUE) schema, so we can plant case-variant rows and then
// hand the DB to normalizeActivityCase to verify it cleans them up.
// The current production schema forbids those duplicates at insert
// time, so we can't simulate an upgrade with openTestDB.
//
// Note: the column set here mirrors the pre-team schema, but
// team_id and project_id have been retro-added so the typed scanners
// (which now expect them) keep working — the migration code is what
// we test, not the schema itself.
func openLegacySchemaDB(t *testing.T) *DB {
	t.Helper()
	const legacy = `
CREATE TABLE activities (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    team_id INTEGER,
    project_id INTEGER,
    archived INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now'))
);
CREATE TABLE sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    activity_id INTEGER NOT NULL,
    team_id INTEGER,
    user_id INTEGER,
    start_at TEXT NOT NULL,
    end_at TEXT,
    note TEXT,
    paused INTEGER NOT NULL DEFAULT 0,
    paused_at TEXT,
    accumulated_seconds INTEGER NOT NULL DEFAULT 0,
    last_resume_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    FOREIGN KEY (activity_id) REFERENCES activities(id) ON DELETE CASCADE
);
CREATE TABLE tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    team_id INTEGER,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now'))
);
CREATE TABLE session_tags (
    session_id INTEGER NOT NULL,
    tag_id INTEGER NOT NULL,
    PRIMARY KEY (session_id, tag_id)
);
CREATE TABLE goals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    activity_id INTEGER NOT NULL,
    period TEXT NOT NULL,
    target_minutes INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    FOREIGN KEY (activity_id) REFERENCES activities(id) ON DELETE CASCADE
);
CREATE TABLE reminders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    activity_id INTEGER NOT NULL,
    every_minutes INTEGER NOT NULL,
    window TEXT,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    FOREIGN KEY (activity_id) REFERENCES activities(id) ON DELETE CASCADE
);
`
	sdb, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	sdb.SetMaxOpenConns(1)
	if _, err := sdb.Exec(legacy); err != nil {
		_ = sdb.Close()
		t.Fatalf("apply legacy schema: %v", err)
	}
	t.Cleanup(func() { _ = sdb.Close() })
	return &DB{sql: &Conn{DB: sdb}}
}