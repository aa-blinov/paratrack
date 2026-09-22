package db

import (
	"strings"
)

// schema is the full DDL for a fresh paratrack database. The same column
// names and types are used by the original Python implementation, so a DB
// created by either tool can be opened by the other (timestamps differ —
// see note in db.go).
const schema = `
CREATE TABLE IF NOT EXISTS activities (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL COLLATE NOCASE UNIQUE,
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
    name TEXT NOT NULL COLLATE NOCASE UNIQUE,
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
	// One-shot data migration: existing rows might have been inserted
	// under mixed-case names (Work, work, WORK …) before the schema
	// switched to COLLATE NOCASE. Lowercase + de-collide so the new
	// uniqueness kicks in cleanly.
	if err := d.normalizeActivityCase(); err != nil {
		return err
	}
	if err := d.normalizeTagCase(); err != nil {
		return err
	}
	return nil
}

// normalizeActivityCase merges case-insensitive duplicates of activity
// names into a single canonical row (lowest id wins) and lowercases
// every name so the COLLATE NOCASE uniqueness introduced in this
// migration actually holds for legacy data.
//
// For each collision group:
//  1. Reassign every session that pointed at a loser over to the winner
//     (ON DELETE CASCADE would otherwise drop the history on step 3).
//  2. Drop the goals / reminders FK rows for the loser.
//  3. DELETE the loser.
//
// After merges, all winners and any non-colliding rows are lowercased
// in place. Running this twice is a no-op.
func (d *DB) normalizeActivityCase() error {
	groups, err := d.groupCaseCollisions(`SELECT id, name FROM activities`)
	if err != nil {
		return err
	}
	for _, grp := range groups {
		if len(grp) <= 1 {
			continue
		}
		// Sort by id ascending so the lowest id is always the winner.
		for i := 1; i < len(grp); i++ {
			for j := i; j > 0 && grp[j-1].id > grp[j].id; j-- {
				grp[j-1], grp[j] = grp[j], grp[j-1]
			}
		}
		winner := grp[0]
		for _, loser := range grp[1:] {
			// Re-point sessions before delete so cascade doesn't drop history.
			if _, err := d.sql.Exec(
				`UPDATE sessions SET activity_id = ? WHERE activity_id = ?`,
				winner.id, loser.id,
			); err != nil {
				return err
			}
			// goals / reminders have ON DELETE CASCADE on activity_id; DELETE
			// will sweep them along with the loser row.
			if _, err := d.sql.Exec(`DELETE FROM activities WHERE id = ?`, loser.id); err != nil {
				return err
			}
		}
	}

	// Lowercase everything that's still alive. Already-lowercase rows
	// hit the equality check below and skip the UPDATE.
	rows, err := d.sql.Query(`SELECT id, name FROM activities`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var survivors []caseRow
	for rows.Next() {
		var r caseRow
		if err := rows.Scan(&r.id, &r.name); err != nil {
			return err
		}
		survivors = append(survivors, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range survivors {
		low := strings.ToLower(r.name)
		if low == r.name {
			continue
		}
		if _, err := d.sql.Exec(`UPDATE activities SET name = ? WHERE id = ?`, low, r.id); err != nil {
			return err
		}
	}
	return nil
}

// normalizeTagCase mirrors normalizeActivityCase for the tags table.
// Loser tags are merged into the winner by rewriting every
// session_tags row from loser → winner (OR IGNORE so duplicate
// (session_id, tag_id) pairs collapse cleanly), then the loser row
// is deleted. After merging, every survivor is lowercased.
func (d *DB) normalizeTagCase() error {
	groups, err := d.groupCaseCollisions(`SELECT id, name FROM tags`)
	if err != nil {
		return err
	}
	for _, grp := range groups {
		if len(grp) <= 1 {
			continue
		}
		for i := 1; i < len(grp); i++ {
			for j := i; j > 0 && grp[j-1].id > grp[j].id; j-- {
				grp[j-1], grp[j] = grp[j], grp[j-1]
			}
		}
		winner := grp[0]
		for _, loser := range grp[1:] {
			// Reassign session_tags before delete. OR IGNORE covers the
			// case where the same session already had the winner tag.
			if _, err := d.sql.Exec(
				`UPDATE OR IGNORE session_tags SET tag_id = ? WHERE tag_id = ?`,
				winner.id, loser.id,
			); err != nil {
				return err
			}
			if _, err := d.sql.Exec(`DELETE FROM session_tags WHERE tag_id = ?`, loser.id); err != nil {
				return err
			}
			if _, err := d.sql.Exec(`DELETE FROM tags WHERE id = ?`, loser.id); err != nil {
				return err
			}
		}
	}

	rows, err := d.sql.Query(`SELECT id, name FROM tags`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var survivors []caseRow
	for rows.Next() {
		var r caseRow
		if err := rows.Scan(&r.id, &r.name); err != nil {
			return err
		}
		survivors = append(survivors, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range survivors {
		low := strings.ToLower(r.name)
		if low == r.name {
			continue
		}
		if _, err := d.sql.Exec(`UPDATE tags SET name = ? WHERE id = ?`, low, r.id); err != nil {
			return err
		}
	}
	return nil
}

// caseRow is the shared shape of an (id, name) tuple the case-normalising
// migrations scan into.
type caseRow struct {
	id   int64
	name string
}

// groupCaseCollisions reads every (id, name) pair from `query` and
// returns them grouped by lower-cased name. The caller iterates each
// group to decide which row wins and which get merged away.
func (d *DB) groupCaseCollisions(query string) ([][]caseRow, error) {
	rows, err := d.sql.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byKey := map[string][]caseRow{}
	var order []string
	for rows.Next() {
		var r caseRow
		if err := rows.Scan(&r.id, &r.name); err != nil {
			return nil, err
		}
		k := strings.ToLower(r.name)
		if _, seen := byKey[k]; !seen {
			order = append(order, k)
		}
		byKey[k] = append(byKey[k], r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([][]caseRow, 0, len(order))
	for _, k := range order {
		out = append(out, byKey[k])
	}
	return out, nil
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
