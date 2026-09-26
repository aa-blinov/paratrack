package db

import (
	"strings"
)

// schema is the full DDL for a fresh paratrack database. The same column
// names and types are used by the original Python implementation, so a DB
// created by either tool can be opened by the other (timestamps differ —
// see note in db.go).
//
// As of the collaboration release (Phase 1–3), the schema adds a user /
// auth / team layer. Every row of an existing table (activities,
// sessions, tags, goals) now carries a `team_id` so a single SQLite file
// can host multiple teams side by side. A fresh install therefore can't
// open a pre-auth database — Open() detects that case and archives the
// file before creating the new schema (hard break, per product call).
const schema = `
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS uniq_users_email ON users(email COLLATE NOCASE);

CREATE TABLE IF NOT EXISTS auth_sessions (
    token TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    expires_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_user ON auth_sessions(user_id);

CREATE TABLE IF NOT EXISTS teams (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    slug TEXT NOT NULL,
    name TEXT NOT NULL,
    owner_id INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX IF NOT EXISTS uniq_teams_slug ON teams(slug COLLATE NOCASE);

CREATE TABLE IF NOT EXISTS memberships (
    team_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    role TEXT NOT NULL CHECK(role IN ('owner', 'member')),
    joined_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    PRIMARY KEY (team_id, user_id),
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_memberships_user ON memberships(user_id);

CREATE TABLE IF NOT EXISTS invites (
    token TEXT PRIMARY KEY,
    team_id INTEGER NOT NULL,
    role TEXT NOT NULL DEFAULT 'member' CHECK(role IN ('owner', 'member')),
    created_by INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    expires_at TEXT NOT NULL,
    accepted_at TEXT,
    accepted_by INTEGER,
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (accepted_by) REFERENCES users(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS idx_invites_team ON invites(team_id);

CREATE TABLE IF NOT EXISTS projects (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id INTEGER NOT NULL,
    slug TEXT NOT NULL COLLATE NOCASE,
    name TEXT NOT NULL,
    color TEXT NOT NULL DEFAULT '#7c8499',
    archived INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
    UNIQUE (team_id, slug)
);
CREATE INDEX IF NOT EXISTS idx_projects_team ON projects(team_id);

CREATE TABLE IF NOT EXISTS activities (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL COLLATE NOCASE,
    team_id INTEGER,
    project_id INTEGER,
    archived INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS idx_activities_team ON activities(team_id);
CREATE INDEX IF NOT EXISTS idx_activities_name ON activities(name COLLATE NOCASE);

CREATE TABLE IF NOT EXISTS sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    activity_id INTEGER NOT NULL,
    team_id INTEGER,
    start_at TEXT NOT NULL,
    end_at TEXT,
    note TEXT,
    paused INTEGER NOT NULL DEFAULT 0,
    paused_at TEXT,
    accumulated_seconds INTEGER NOT NULL DEFAULT 0,
    last_resume_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    FOREIGN KEY (activity_id) REFERENCES activities(id) ON DELETE CASCADE,
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_sessions_activity ON sessions(activity_id);
CREATE INDEX IF NOT EXISTS idx_sessions_team     ON sessions(team_id);
CREATE INDEX IF NOT EXISTS idx_sessions_start    ON sessions(start_at);
CREATE INDEX IF NOT EXISTS idx_sessions_end      ON sessions(end_at);

CREATE TABLE IF NOT EXISTS tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL COLLATE NOCASE,
    team_id INTEGER,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
    UNIQUE (team_id, name)
);
CREATE INDEX IF NOT EXISTS idx_tags_team ON tags(team_id);
CREATE INDEX IF NOT EXISTS idx_tags_name ON tags(name COLLATE NOCASE);

CREATE TABLE IF NOT EXISTS session_tags (
    session_id INTEGER NOT NULL,
    tag_id INTEGER NOT NULL,
    PRIMARY KEY (session_id, tag_id),
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE,
    FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_session_tags_session ON session_tags(session_id);
CREATE INDEX IF NOT EXISTS idx_session_tags_tag     ON session_tags(tag_id);

CREATE TABLE IF NOT EXISTS goals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    activity_id INTEGER NOT NULL,
    team_id INTEGER NOT NULL DEFAULT 0,
    period TEXT NOT NULL CHECK(period IN ('daily', 'weekly', 'monthly')),
    target_minutes INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    FOREIGN KEY (activity_id) REFERENCES activities(id) ON DELETE CASCADE,
    UNIQUE (team_id, activity_id, period)
);
CREATE INDEX IF NOT EXISTS idx_goals_team ON goals(team_id);

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

CREATE TABLE IF NOT EXISTS password_reset_tokens (
    token TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    used_at TEXT,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_password_reset_user ON password_reset_tokens(user_id);

CREATE TABLE IF NOT EXISTS invoices (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id INTEGER NOT NULL,
    number TEXT NOT NULL,
    client_name TEXT NOT NULL DEFAULT '',
    period_start TEXT NOT NULL,
    period_end TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    notes TEXT NOT NULL DEFAULT '',
    payment_url TEXT DEFAULT '',
    stripe_session_id TEXT DEFAULT '',
    paid_at TEXT,
    created_at TEXT NOT NULL,
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
    UNIQUE (team_id, number)
);
CREATE INDEX IF NOT EXISTS idx_invoices_team ON invoices(team_id);

CREATE TABLE IF NOT EXISTS invoice_lines (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    invoice_id INTEGER NOT NULL,
    label TEXT NOT NULL,
    detail TEXT NOT NULL DEFAULT '',
    seconds INTEGER NOT NULL DEFAULT 0,
    rate_cents INTEGER NOT NULL DEFAULT 0,
    amount_cents INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (invoice_id) REFERENCES invoices(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_invoice_lines_invoice ON invoice_lines(invoice_id);


CREATE TABLE IF NOT EXISTS payroll_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id INTEGER NOT NULL,
    number TEXT NOT NULL,
    period_start TEXT NOT NULL,
    period_end TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    notes TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
    UNIQUE (team_id, number)
);
CREATE INDEX IF NOT EXISTS idx_payroll_runs_team ON payroll_runs(team_id);

CREATE TABLE IF NOT EXISTS payroll_lines (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    label TEXT NOT NULL DEFAULT '',
    seconds INTEGER NOT NULL DEFAULT 0,
    rate_cents INTEGER NOT NULL DEFAULT 0,
    amount_cents INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (run_id) REFERENCES payroll_runs(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_payroll_lines_run ON payroll_lines(run_id);

CREATE TABLE IF NOT EXISTS schedule_entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    project_id INTEGER NOT NULL,
    day TEXT NOT NULL,
    minutes INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
    UNIQUE (team_id, user_id, project_id, day)
);
CREATE INDEX IF NOT EXISTS idx_schedule_team ON schedule_entries(team_id, day);

CREATE TABLE IF NOT EXISTS push_subscriptions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    endpoint TEXT NOT NULL UNIQUE,
    p256dh TEXT NOT NULL,
    auth TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_push_subs_team ON push_subscriptions(team_id);

CREATE TABLE IF NOT EXISTS push_keys (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    public_key TEXT NOT NULL,
    private_key TEXT NOT NULL
);

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
	{"activities", "project_id", "INTEGER"},
	// Estimates vs actual (Wave 1).
	{"projects", "estimate_minutes", "INTEGER"},
	{"projects", "billable_rate_cents", "INTEGER"},
	{"projects", "billable", "INTEGER NOT NULL DEFAULT 1"},
	{"teams", "stripe_key", "TEXT"},
	{"teams", "stripe_webhook_secret", "TEXT"},
	{"invoices", "payment_url", "TEXT"},
	{"invoices", "stripe_session_id", "TEXT"},
	{"invoices", "paid_at", "TEXT"},
	{"sessions", "user_id", "INTEGER"},
	{"memberships", "hourly_pay_cents", "INTEGER"},
	{"memberships", "capacity_minutes", "INTEGER"},
}

// uniqueMigrations creates UNIQUE / lookup indexes that the original
// schema didn't include or that depend on a column being added by
// columnMigrations. Each entry is idempotent: IF NOT EXISTS lets us
// run it on every startup without harm.
//
// Note: the goal-uniqueness guarantee now lives in the CREATE TABLE
// clause (`UNIQUE (team_id, activity_id, period)`); the historical
// (activity_id, period) index is intentionally not recreated here
// because it would conflict with the ON CONFLICT(team_id, activity_id,
// period) DO UPDATE used by UpsertGoal.
var uniqueMigrations = []string{
	`CREATE INDEX IF NOT EXISTS idx_activities_project ON activities(project_id)`,
	// Older DBs predate the password-reset table; IF NOT EXISTS makes
	// this safe to re-run on every startup.
	`CREATE TABLE IF NOT EXISTS password_reset_tokens (
	    token TEXT PRIMARY KEY,
	    user_id INTEGER NOT NULL,
	    created_at TEXT NOT NULL,
	    expires_at TEXT NOT NULL,
	    used_at TEXT,
	    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_password_reset_user ON password_reset_tokens(user_id)`,
	// Saved /stats filter presets (Wave 1).
	`CREATE TABLE IF NOT EXISTS saved_reports (
	    id INTEGER PRIMARY KEY AUTOINCREMENT,
	    team_id INTEGER NOT NULL,
	    name TEXT NOT NULL,
	    period TEXT NOT NULL DEFAULT 'today',
	    project_slug TEXT NOT NULL DEFAULT '',
	    tag TEXT NOT NULL DEFAULT '',
	    created_by INTEGER,
	    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
	    UNIQUE (team_id, name)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_saved_reports_team ON saved_reports(team_id)`,
	// Wave 2: API tokens (extension / CLI bearer auth).
	`CREATE TABLE IF NOT EXISTS api_tokens (
	    id INTEGER PRIMARY KEY AUTOINCREMENT,
	    user_id INTEGER NOT NULL,
	    name TEXT NOT NULL,
	    token_hash TEXT NOT NULL UNIQUE,
	    prefix TEXT NOT NULL,
	    created_at TEXT NOT NULL,
	    last_used_at TEXT,
	    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_api_tokens_user ON api_tokens(user_id)`,
	// Wave 2: integrations (github / trello).
	`CREATE TABLE IF NOT EXISTS integrations (
	    id INTEGER PRIMARY KEY AUTOINCREMENT,
	    team_id INTEGER NOT NULL,
	    provider TEXT NOT NULL,
	    name TEXT NOT NULL,
	    secret TEXT NOT NULL DEFAULT '',
	    config TEXT NOT NULL DEFAULT '{}',
	    created_at TEXT NOT NULL,
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
	    UNIQUE (team_id, provider, name)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_integrations_team ON integrations(team_id)`,
	// External work items imported from an integration.
	`CREATE TABLE IF NOT EXISTS external_tasks (
	    id INTEGER PRIMARY KEY AUTOINCREMENT,
	    integration_id INTEGER NOT NULL,
	    external_id TEXT NOT NULL,
	    title TEXT NOT NULL,
	    url TEXT NOT NULL DEFAULT '',
	    status TEXT NOT NULL DEFAULT 'open',
	    activity_id INTEGER,
	    created_at TEXT NOT NULL,
	    FOREIGN KEY (integration_id) REFERENCES integrations(id) ON DELETE CASCADE,
	    FOREIGN KEY (activity_id) REFERENCES activities(id) ON DELETE SET NULL,
	    UNIQUE (integration_id, external_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_external_tasks_integration ON external_tasks(integration_id)`,
	// Wave 3: invoices.
	`CREATE TABLE IF NOT EXISTS invoices (
	    id INTEGER PRIMARY KEY AUTOINCREMENT,
	    team_id INTEGER NOT NULL,
	    number TEXT NOT NULL,
	    client_name TEXT NOT NULL DEFAULT '',
	    period_start TEXT NOT NULL,
	    period_end TEXT NOT NULL,
	    status TEXT NOT NULL DEFAULT 'draft',
	    notes TEXT NOT NULL DEFAULT '',
	    payment_url TEXT DEFAULT '',
	    stripe_session_id TEXT DEFAULT '',
	    paid_at TEXT,
	    created_at TEXT NOT NULL,
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
	    UNIQUE (team_id, number)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_invoices_team ON invoices(team_id)`,
	`CREATE TABLE IF NOT EXISTS invoice_lines (
	    id INTEGER PRIMARY KEY AUTOINCREMENT,
	    invoice_id INTEGER NOT NULL,
	    label TEXT NOT NULL,
	    detail TEXT NOT NULL DEFAULT '',
	    seconds INTEGER NOT NULL DEFAULT 0,
	    rate_cents INTEGER NOT NULL DEFAULT 0,
	    amount_cents INTEGER NOT NULL DEFAULT 0,
	    FOREIGN KEY (invoice_id) REFERENCES invoices(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_invoice_lines_invoice ON invoice_lines(invoice_id)`,
	// Wave 4: audit log (enterprise).
	`CREATE TABLE IF NOT EXISTS audit_log (
	    id INTEGER PRIMARY KEY AUTOINCREMENT,
	    team_id INTEGER NOT NULL DEFAULT 0,
	    user_id INTEGER NOT NULL DEFAULT 0,
	    action TEXT NOT NULL,
	    target TEXT NOT NULL DEFAULT '',
	    meta TEXT NOT NULL DEFAULT '',
	    ip TEXT NOT NULL DEFAULT '',
	    created_at TEXT NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_audit_log_team ON audit_log(team_id, created_at)`,
	// Wave 4: outbound webhooks.
	`CREATE TABLE IF NOT EXISTS webhooks (
	    id INTEGER PRIMARY KEY AUTOINCREMENT,
	    team_id INTEGER NOT NULL,
	    url TEXT NOT NULL,
	    secret TEXT NOT NULL DEFAULT '',
	    events TEXT NOT NULL DEFAULT 'session.stopped,invoice.created',
	    active INTEGER NOT NULL DEFAULT 1,
	    created_at TEXT NOT NULL,
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_webhooks_team ON webhooks(team_id)`,
	`CREATE TABLE IF NOT EXISTS webhook_deliveries (
	    id INTEGER PRIMARY KEY AUTOINCREMENT,
	    webhook_id INTEGER NOT NULL,
	    event TEXT NOT NULL,
	    payload TEXT NOT NULL,
	    status INTEGER NOT NULL DEFAULT 0,
	    error TEXT NOT NULL DEFAULT '',
	    created_at TEXT NOT NULL,
	    FOREIGN KEY (webhook_id) REFERENCES webhooks(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_webhook_deliveries ON webhook_deliveries(webhook_id)`,
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