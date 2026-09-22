package db

// schema is the full DDL for a fresh paratrack database. The same column
// names and types are used by the original Python implementation, so a DB
// created by either tool can be opened by the other (timestamps differ —
// see note in db.go).
const schema = `
CREATE TABLE IF NOT EXISTS activities (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    archived INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now'))
);

CREATE TABLE IF NOT EXISTS sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    activity_id INTEGER NOT NULL,
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

CREATE TABLE IF NOT EXISTS tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now'))
);

CREATE TABLE IF NOT EXISTS session_tags (
    session_id INTEGER NOT NULL,
    tag_id INTEGER NOT NULL,
    PRIMARY KEY (session_id, tag_id),
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE,
    FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS goals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    activity_id INTEGER NOT NULL,
    period TEXT NOT NULL CHECK(period IN ('daily', 'weekly', 'monthly')),
    target_minutes INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    FOREIGN KEY (activity_id) REFERENCES activities(id) ON DELETE CASCADE,
    UNIQUE (activity_id, period)
);

CREATE TABLE IF NOT EXISTS reminders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    activity_id INTEGER NOT NULL,
    every_minutes INTEGER NOT NULL,
    window TEXT,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    FOREIGN KEY (activity_id) REFERENCES activities(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_sessions_activity ON sessions(activity_id);
CREATE INDEX IF NOT EXISTS idx_sessions_start   ON sessions(start_at);
CREATE INDEX IF NOT EXISTS idx_sessions_end     ON sessions(end_at);
CREATE INDEX IF NOT EXISTS idx_session_tags_session ON session_tags(session_id);
CREATE INDEX IF NOT EXISTS idx_session_tags_tag     ON session_tags(tag_id);
`

// columnMigrations ensures older databases (e.g. created before pause/resume
// support) get the required columns added. Safe to run repeatedly.
var columnMigrations = []struct {
	table  string
	column string
	decl   string
}{
	{"sessions", "paused", "INTEGER NOT NULL DEFAULT 0"},
	{"sessions", "paused_at", "TEXT"},
	{"sessions", "accumulated_seconds", "INTEGER NOT NULL DEFAULT 0"},
	{"sessions", "last_resume_at", "TEXT"},
}

// uniqueMigrations creates UNIQUE indexes that the original schema
// didn't include. Each entry is idempotent: IF NOT EXISTS lets us run
// it on every startup without harm.
var uniqueMigrations = []string{
	`CREATE UNIQUE INDEX IF NOT EXISTS uniq_goals_activity_period ON goals(activity_id, period)`,
}

func (d *DB) applyMigrations() error {
	for _, m := range columnMigrations {
		exists, err := d.columnExists(m.table, m.column)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if _, err := d.sql.Exec("ALTER TABLE " + m.table + " ADD COLUMN " + m.column + " " + m.decl); err != nil {
			return err
		}
	}
	for _, stmt := range uniqueMigrations {
		if _, err := d.sql.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) columnExists(table, column string) (bool, error) {
	rows, err := d.sql.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    *string
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
