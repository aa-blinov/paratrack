package db

// columnMigrations ensures older databases (e.g. created before pause/resume
// support) get the required columns added. Safe to run repeatedly.
var columnMigrations = []struct {
	table  string
	column string
	decl   string
}{
	{"sessions", "paused", "BIGINT NOT NULL DEFAULT 0"},
	{"sessions", "paused_at", "TEXT"},
	{"sessions", "accumulated_seconds", "BIGINT NOT NULL DEFAULT 0"},
	{"sessions", "last_resume_at", "TEXT"},
	{"activities", "project_id", "BIGINT"},
	// A won deal's project (CRM-13). It is set once, and the project's own deletion
	// clears it, so a deal never points at a project that is gone.
	{"crm_deals", "project_id", "BIGINT REFERENCES projects(id) ON DELETE SET NULL"},
	// When a lead was first answered (CRM-16): the time of its first activity, set once and
	// left NULL until then. The backfill in migrateLeadFirstResponses fills the leads that
	// already had one.
	{"crm_leads", "first_response_at", "TEXT"},
	// The colour an activity wears. It used to be hashed from the name into
	// a ten-slot palette, so with more than ten activities two of them
	// looked identical and the mark identified nothing. Stored now; the
	// backfill in migrateActivityColors gives every activity its own slot.
	{"activities", "color", "TEXT"},
	// Estimates vs actual (Wave 1).
	{"projects", "estimate_minutes", "BIGINT"},
	{"projects", "deadline", "TEXT"},
	{"projects", "lead_user_id", "BIGINT"},
	{"projects", "deadline_reminded", "TEXT"},
	// An activity may belong to one task; a task holds many activities.
	{"activities", "task_id", "BIGINT"},
	{"tasks", "due_reminder", "TEXT"},
	{"tasks", "description", "TEXT NOT NULL DEFAULT ''"},
	{"tasks", "external_ref", "TEXT"},
	// The task a subtask belongs to, 0 for a top-level task (TASK-09).
	{"tasks", "parent_id", "BIGINT NOT NULL DEFAULT 0"},
	{"tasks", "external_url", "TEXT"},
	{"projects", "billable_rate_cents", "BIGINT"},
	{"projects", "billable", "BIGINT NOT NULL DEFAULT 1"},
	{"teams", "stripe_key", "TEXT"},
	{"teams", "stripe_webhook_secret", "TEXT"},
	{"invoices", "payment_url", "TEXT"},
	{"invoices", "stripe_session_id", "TEXT"},
	{"invoices", "paid_at", "TEXT"},
	{"sessions", "user_id", "BIGINT"},
	{"memberships", "hourly_pay_cents", "BIGINT"},
	{"memberships", "capacity_minutes", "BIGINT"},
	{"memberships", "admin_minutes", "BIGINT"},
	// Currency: the workspace default, a project override ('' = inherit),
	// and the currency fixed on each document when it is created.
	{"teams", "currency", "TEXT NOT NULL DEFAULT 'RUB'"},
	// The workspace's own time zone and first day of the week; blank leaves each
	// member's own choice in force (ADM-07).
	{"teams", "timezone", "TEXT NOT NULL DEFAULT ''"},
	{"teams", "week_start", "TEXT NOT NULL DEFAULT ''"},
	// Days after which time that starts in the past closes for edits (timesheet-autolock).
	// Zero keeps the past open; existing workspaces stay at zero.
	{"teams", "autolock_days", "INTEGER NOT NULL DEFAULT 0"},
	{"projects", "currency", "TEXT NOT NULL DEFAULT ''"},
	{"invoices", "currency", "TEXT NOT NULL DEFAULT ''"},
	{"payroll_runs", "currency", "TEXT NOT NULL DEFAULT ''"},
	// Imported history: "toggl:123" etc., so a re-import adds nothing twice.
	{"sessions", "external_id", "TEXT"},
	// Document details: the issuer's requisites and VAT line on the
	// workspace, copied onto each invoice with the client's own details.
	{"teams", "requisites", "TEXT NOT NULL DEFAULT ''"},
	// Whether clients' invoices leave out the members' names on by-person lines (FIN-15).
	{"teams", "hide_executor_names", "BIGINT NOT NULL DEFAULT 0"},
	// VAT rate in percent, 0 for none; an invoice keeps the rate it was drafted with (FIN-16).
	{"teams", "vat_rate", "BIGINT NOT NULL DEFAULT 0"},
	{"invoices", "vat_rate", "BIGINT NOT NULL DEFAULT 0"},
	{"teams", "vat_note", "TEXT NOT NULL DEFAULT ''"},
	{"invoices", "seller_details", "TEXT NOT NULL DEFAULT ''"},
	{"invoices", "client_details", "TEXT NOT NULL DEFAULT ''"},
	{"invoices", "vat_note", "TEXT NOT NULL DEFAULT ''"},
	// A project points at a client card (0 = keep using the snapshot columns
	// above) and carries a monetary ceiling. The snapshot columns are left in
	// place on purpose: they are what an already-sent invoice was addressed to,
	// and renaming a client must not rewrite history.
	{"projects", "client_id", "BIGINT"},
	{"projects", "budget_cents", "BIGINT"},
	// Freelancer flow: a project remembers its client (name, details,
	// email) for the next invoice; a billed session points at its invoice
	// so it isn't billed twice and can't change under a sent document;
	// a paid invoice can carry the "Мой налог" receipt.
	{"projects", "client_name", "TEXT NOT NULL DEFAULT ''"},
	{"projects", "client_details", "TEXT NOT NULL DEFAULT ''"},
	{"projects", "client_email", "TEXT NOT NULL DEFAULT ''"},
	{"sessions", "invoice_id", "BIGINT"},
	// The session's own project. Empty means the session takes its activity's project.
	// No foreign key, the same as activities.project_id: a deleted project leaves its
	// references behind for the reports to drop.
	{"sessions", "project_id", "BIGINT"},
	{"invoices", "project_id", "BIGINT"},
	{"invoices", "client_email", "TEXT NOT NULL DEFAULT ''"},
	{"invoices", "due_date", "TEXT NOT NULL DEFAULT ''"},
	{"invoices", "receipt", "TEXT NOT NULL DEFAULT ''"},
	{"invoices", "by_person", "BIGINT NOT NULL DEFAULT 0"}, // lines split per team member
	{"invoices", "revision", "BIGINT NOT NULL DEFAULT 1"},
	{"invoice_stripe_sessions", "active", "BIGINT NOT NULL DEFAULT 1"},
	// Personal preferences (JSON, see web/prefs.go).
	{"users", "prefs", "TEXT NOT NULL DEFAULT ''"},
	// Notification topics this person switched off, comma separated (see
	// db/preferences.go). Empty means nothing is off, which is what every
	// existing row already means, so no backfill is needed.
	{"users", "muted_notifications", "TEXT NOT NULL DEFAULT ''"},
	// Webhook delivery bodies: the request body was already kept as
	// `payload`; the receiver's response is kept too so an integrator can
	// see why their endpoint rejected the call. The *_truncated flags say the
	// stored text was clipped at 2000 bytes.
	{"webhook_deliveries", "request_truncated", "BIGINT NOT NULL DEFAULT 0"},
	{"webhook_deliveries", "response", "TEXT NOT NULL DEFAULT ''"},
	{"webhook_deliveries", "response_truncated", "BIGINT NOT NULL DEFAULT 0"},
	// Saved /stats presets filter by row id, so a rename keeps the filter and
	// a deleted row reads back as no filter at all. The text columns stay for
	// rows written before the migration.
	{"saved_reports", "project_id", "BIGINT"},
	{"saved_reports", "tag_id", "BIGINT"},
	// Billing rules: round each invoice line to N minutes (0 = 0.01 h),
	// "up" or "nearest"; invoice number prefix; logo for documents.
	{"teams", "round_minutes", "BIGINT NOT NULL DEFAULT 0"},
	{"teams", "round_mode", "TEXT NOT NULL DEFAULT 'nearest'"},
	{"teams", "invoice_prefix", "TEXT NOT NULL DEFAULT 'INV'"},
	{"teams", "logo", "TEXT NOT NULL DEFAULT ''"},
	// Sections the workspace uses ("" = all, "none" = core only).
	{"teams", "modules", "TEXT NOT NULL DEFAULT ''"},
	// Whether the workspace has picked its sections. A workspace created new
	// starts at 0 and the menu waits at /welcome; existing ones are 1, so they
	// keep the menu they always had.
	{"teams", "sections_chosen", "BIGINT NOT NULL DEFAULT 1"},
	// Activity lookup key: lowercase in Go (Unicode), the name keeps case.
	{"activities", "name_key", "TEXT"},
	// API tokens: the workspace they act in, optional expiry, read-only.
	{"api_tokens", "team_id", "BIGINT"},
	{"api_tokens", "expires_at", "TEXT"},
	{"api_tokens", "read_only", "BIGINT NOT NULL DEFAULT 0"},
	{"webhook_event_outbox", "webhook_ids", "TEXT NOT NULL DEFAULT ''"},
	{"invites", "sealed_token", "TEXT NOT NULL DEFAULT ''"},
	// The address an invitation was made for, lower-cased. Empty for links
	// made without one, which never count as a duplicate.
	{"invites", "email", "TEXT NOT NULL DEFAULT ''"},
	{"integrations", "sync_generation", "BIGINT NOT NULL DEFAULT 0"},
	// The last failed sync, shown until a sync succeeds. Empty means none.
	{"integrations", "last_sync_error", "TEXT NOT NULL DEFAULT ''"},
}

// compatibilityStatements create or update schema objects introduced after
// the original database schema. They are idempotent and run on every startup.
var compatibilityStatements = []string{
	`CREATE TABLE IF NOT EXISTS invoice_email_outbox (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    team_id BIGINT NOT NULL,
	    invoice_id BIGINT NOT NULL,
	    recipient TEXT NOT NULL,
	    payload TEXT NOT NULL,
	    status TEXT NOT NULL DEFAULT 'pending',
	    attempts BIGINT NOT NULL DEFAULT 0,
	    available_at TEXT NOT NULL,
	    locked_at TEXT,
	    last_error TEXT NOT NULL DEFAULT '',
	    created_at TEXT NOT NULL,
	    completed_at TEXT,
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
	    FOREIGN KEY (invoice_id) REFERENCES invoices(id) ON DELETE CASCADE
	)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_invoice_email_active ON invoice_email_outbox(invoice_id) WHERE status IN ('pending', 'processing')`,
	`CREATE INDEX IF NOT EXISTS idx_invoice_email_claim ON invoice_email_outbox(status, available_at)`,
	// Reminder letters share the outbox and have no invoice, so invoice_id must allow NULL.
	// Databases created before that have it NOT NULL; the catalog is read first, so the
	// ALTER runs once.
	`DO $$ BEGIN
	   IF EXISTS (
	     SELECT 1 FROM pg_attribute
	     WHERE attrelid = 'invoice_email_outbox'::regclass AND attname = 'invoice_id' AND attnotnull
	   ) THEN
	     ALTER TABLE invoice_email_outbox ALTER COLUMN invoice_id DROP NOT NULL;
	   END IF;
	 END $$`,
	`CREATE TABLE IF NOT EXISTS invoice_stripe_sessions (
	    stripe_session_id TEXT PRIMARY KEY,
	    team_id BIGINT NOT NULL,
	    invoice_id BIGINT NOT NULL,
	    created_at TEXT NOT NULL,
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
	    FOREIGN KEY (invoice_id) REFERENCES invoices(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_invoice_stripe_sessions_invoice ON invoice_stripe_sessions(invoice_id)`,
	// A client's link to an invoice: only the hash of the token is kept, and a link
	// stops opening the invoice at expires_at (FIN-13).
	`CREATE TABLE IF NOT EXISTS invoice_share_links (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    team_id BIGINT NOT NULL,
	    invoice_id BIGINT NOT NULL,
	    token_hash TEXT NOT NULL,
	    created_by BIGINT NOT NULL,
	    created_at TEXT NOT NULL,
	    expires_at TEXT NOT NULL,
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
	    FOREIGN KEY (invoice_id) REFERENCES invoices(id) ON DELETE CASCADE
	)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS uniq_invoice_share_token ON invoice_share_links(token_hash)`,
	// The start and the end of a member's working day (TIME-06).
	`CREATE TABLE IF NOT EXISTS workday_marks (
	    team_id BIGINT NOT NULL,
	    user_id BIGINT NOT NULL,
	    day TEXT NOT NULL,
	    started_at TEXT NOT NULL,
	    ended_at TEXT NOT NULL DEFAULT '',
	    PRIMARY KEY (team_id, user_id, day),
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
	    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	)`,
	// A member's mark on a day: an overload or an idle day, one per member and day, which
	// the manager sees on the team week (TIME-13).
	`CREATE TABLE IF NOT EXISTS day_marks (
	    team_id BIGINT NOT NULL,
	    user_id BIGINT NOT NULL,
	    day TEXT NOT NULL,
	    kind TEXT NOT NULL,
	    note TEXT NOT NULL DEFAULT '',
	    created_at TEXT NOT NULL,
	    PRIMARY KEY (team_id, user_id, day),
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
	    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	)`,
	// Big teams: every member view filters team + person + time; the
	// unbilled total wants closed sessions not on an invoice.
	`CREATE INDEX IF NOT EXISTS idx_sessions_team_start ON sessions(team_id, start_at)`,
	`CREATE INDEX IF NOT EXISTS idx_sessions_team_user_start ON sessions(team_id, user_id, start_at)`,
	`CREATE INDEX IF NOT EXISTS idx_sessions_activity_start ON sessions(activity_id, start_at)`,
	`CREATE INDEX IF NOT EXISTS idx_sessions_invoice ON sessions(invoice_id) WHERE invoice_id IS NOT NULL`,
	`CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project_id) WHERE project_id IS NOT NULL`,
	`CREATE INDEX IF NOT EXISTS idx_sessions_unbilled ON sessions(team_id, activity_id) INCLUDE (start_at, end_at, accumulated_seconds) WHERE invoice_id IS NULL AND end_at IS NOT NULL`,
	// One row per name in a team. The timer, goals and reports find an activity by
	// its name; a second row would split the time from its goal. Rows written
	// before the constraint were merged first.
	`DROP INDEX IF EXISTS idx_activities_key`,
	`CREATE UNIQUE INDEX IF NOT EXISTS activities_team_name_key ON activities(team_id, name_key) WHERE name_key IS NOT NULL`,
	// Someone who left keeps their pay rate for the time they worked, so
	// the next pay run still pays it.
	`CREATE TABLE IF NOT EXISTS former_members (
	    team_id BIGINT NOT NULL,
	    user_id BIGINT NOT NULL,
	    hourly_pay_cents BIGINT,
	    left_at TEXT NOT NULL,
	    PRIMARY KEY (team_id, user_id)
	)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_external ON sessions(team_id, external_id) WHERE external_id IS NOT NULL`,
	`CREATE INDEX IF NOT EXISTS idx_activities_project ON activities(project_id)`,
	// Older DBs predate the password-reset table; IF NOT EXISTS makes
	// this safe to re-run on every startup.
	`CREATE TABLE IF NOT EXISTS password_reset_tokens (
	    token TEXT PRIMARY KEY,
	    user_id BIGINT NOT NULL,
	    created_at TEXT NOT NULL,
	    expires_at TEXT NOT NULL,
	    used_at TEXT,
	    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_password_reset_user ON password_reset_tokens(user_id)`,
	// Saved /stats filter presets (Wave 1).
	`CREATE TABLE IF NOT EXISTS saved_reports (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    team_id BIGINT NOT NULL,
	    name TEXT NOT NULL,
	    period TEXT NOT NULL DEFAULT 'today',
	    project_slug TEXT NOT NULL DEFAULT '',
	    tag TEXT NOT NULL DEFAULT '',
	    created_by BIGINT,
	    created_at TEXT NOT NULL DEFAULT (to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS')),
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
	    UNIQUE (team_id, name)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_saved_reports_team ON saved_reports(team_id)`,
	// Wave 2: API tokens (extension / CLI bearer auth).
	`CREATE TABLE IF NOT EXISTS api_tokens (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    user_id BIGINT NOT NULL,
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
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    team_id BIGINT NOT NULL,
	    provider TEXT NOT NULL,
	    name TEXT NOT NULL,
	    secret TEXT NOT NULL DEFAULT '',
	    config TEXT NOT NULL DEFAULT '{}',
	    sync_generation BIGINT NOT NULL DEFAULT 0,
	    last_sync_error TEXT NOT NULL DEFAULT '',
	    created_at TEXT NOT NULL,
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
	    UNIQUE (team_id, provider, name)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_integrations_team ON integrations(team_id)`,
	// External work items imported from an integration.
	`CREATE TABLE IF NOT EXISTS external_tasks (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    integration_id BIGINT NOT NULL,
	    external_id TEXT NOT NULL,
	    title TEXT NOT NULL,
	    url TEXT NOT NULL DEFAULT '',
	    status TEXT NOT NULL DEFAULT 'open',
	    activity_id BIGINT,
	    created_at TEXT NOT NULL,
	    FOREIGN KEY (integration_id) REFERENCES integrations(id) ON DELETE CASCADE,
	    FOREIGN KEY (activity_id) REFERENCES activities(id) ON DELETE SET NULL,
	    UNIQUE (integration_id, external_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_external_tasks_integration ON external_tasks(integration_id)`,
	// Wave 3: invoices.
	`CREATE TABLE IF NOT EXISTS invoices (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    team_id BIGINT NOT NULL,
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
	// Approved timesheet weeks. A row closes its window for writes to time that
	// starts inside it, and pay runs count only time inside approved windows.
	`CREATE TABLE IF NOT EXISTS timesheet_approvals (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    team_id BIGINT NOT NULL,
	    starts_at TEXT NOT NULL,
	    ends_at TEXT NOT NULL,
	    approved_by BIGINT NOT NULL,
	    approved_at TEXT NOT NULL,
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
	    UNIQUE (team_id, starts_at)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_timesheet_approvals_team ON timesheet_approvals(team_id)`,
	// The payment journal of an invoice (FIN-08). Rows are only added, so what
	// the client paid stays as it was booked.
	`CREATE TABLE IF NOT EXISTS invoice_payments (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    team_id BIGINT NOT NULL,
	    invoice_id BIGINT NOT NULL,
	    paid_on TEXT NOT NULL,
	    amount_cents BIGINT NOT NULL,
	    note TEXT NOT NULL DEFAULT '',
	    source TEXT NOT NULL DEFAULT 'manual',
	    created_at TEXT NOT NULL,
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
	    FOREIGN KEY (invoice_id) REFERENCES invoices(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_invoice_payments_invoice ON invoice_payments(team_id, invoice_id)`,
	// A project's weekly norm in minutes (PLAN-02). One row per project; a zero
	// norm removes the row.
	`CREATE TABLE IF NOT EXISTS project_weekly_norms (
	    team_id BIGINT NOT NULL,
	    project_id BIGINT PRIMARY KEY,
	    minutes BIGINT NOT NULL,
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
	    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
	)`,
	// The work on a project. Archived, never deleted: its time stays attached.
	`CREATE TABLE IF NOT EXISTS tasks (
		id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
		team_id BIGINT NOT NULL,
		project_id BIGINT NOT NULL,
		title TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'todo',
		position BIGINT NOT NULL DEFAULT 0,
		assignee_user_id BIGINT,
		due_date TEXT,
		estimate_minutes BIGINT,
		created_by BIGINT,
		archived BIGINT NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_tasks_team_project ON tasks(team_id, project_id)`,
	// A task's checklist: steps that are done or not, in the order they were added (TASK-10).
	`CREATE TABLE IF NOT EXISTS task_checklist_items (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    team_id BIGINT NOT NULL,
	    task_id BIGINT NOT NULL,
	    body TEXT NOT NULL,
	    done BIGINT NOT NULL DEFAULT 0,
	    position BIGINT NOT NULL DEFAULT 0,
	    created_at TEXT NOT NULL,
	    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_task_checklist_task ON task_checklist_items(team_id, task_id)`,
	// A task waits for another: task_id cannot be done before depends_on_id is (TASK-11).
	`CREATE TABLE IF NOT EXISTS task_dependencies (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    team_id BIGINT NOT NULL,
	    task_id BIGINT NOT NULL,
	    depends_on_id BIGINT NOT NULL,
	    created_at TEXT NOT NULL,
	    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE,
	    FOREIGN KEY (depends_on_id) REFERENCES tasks(id) ON DELETE CASCADE
	)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS task_dependencies_pair ON task_dependencies(task_id, depends_on_id)`,
	// A member asks for a change to one of their timesheet cells; a manager approves or refuses it (TIME-19).
	`CREATE TABLE IF NOT EXISTS time_edit_requests (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    team_id BIGINT NOT NULL,
	    user_id BIGINT NOT NULL,
	    activity_id BIGINT NOT NULL,
	    day TEXT NOT NULL,
	    minutes BIGINT NOT NULL,
	    reason TEXT NOT NULL,
	    status TEXT NOT NULL DEFAULT 'pending',
	    decided_by BIGINT NOT NULL DEFAULT 0,
	    decision_note TEXT NOT NULL DEFAULT '',
	    created_at TEXT NOT NULL,
	    decided_at TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE INDEX IF NOT EXISTS idx_time_edit_requests_team_status ON time_edit_requests(team_id, status)`,
	// A file attached to a task, stored with the task and removed with it (TASK-13).
	`CREATE TABLE IF NOT EXISTS task_attachments (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    team_id BIGINT NOT NULL,
	    task_id BIGINT NOT NULL,
	    name TEXT NOT NULL,
	    content_type TEXT NOT NULL,
	    size BIGINT NOT NULL,
	    data BYTEA NOT NULL,
	    created_by BIGINT NOT NULL DEFAULT 0,
	    created_at TEXT NOT NULL,
	    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_task_attachments_task ON task_attachments(team_id, task_id)`,
	// Who follows a task: a watcher hears of its changes, a co-assignee also works on it (TASK-12).
	`CREATE TABLE IF NOT EXISTS task_watchers (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    team_id BIGINT NOT NULL,
	    task_id BIGINT NOT NULL,
	    user_id BIGINT NOT NULL,
	    role TEXT NOT NULL DEFAULT 'watcher',
	    created_at TEXT NOT NULL,
	    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
	)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS task_watchers_pair ON task_watchers(task_id, user_id)`,
	// A task's feed: who wrote what and when (TASK-02). Removed with its task.
	`CREATE TABLE IF NOT EXISTS task_comments (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    team_id BIGINT NOT NULL,
	    task_id BIGINT NOT NULL,
	    user_id BIGINT NOT NULL,
	    body TEXT NOT NULL,
	    created_at TEXT NOT NULL,
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
	    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_task_comments_task ON task_comments(team_id, task_id)`,
	// The task section is on for every workspace that chose its sections. A
	// workspace that switched every section off keeps that choice ("none"), and
	// one that never chose (empty) already sees everything.
	`UPDATE teams SET modules = modules || ',tasks' WHERE modules <> '' AND modules <> 'none' AND modules NOT LIKE '%tasks%'`,
	// A person's project line on the schedule. Hours live in schedule_entries by
	// week; the line itself stays until someone takes it off, so a project a
	// person is on with no hours yet does not vanish on reload.
	`CREATE TABLE IF NOT EXISTS schedule_lines (
		team_id BIGINT NOT NULL,
		user_id BIGINT NOT NULL,
		project_id BIGINT NOT NULL,
		created_at TEXT NOT NULL,
		PRIMARY KEY (team_id, user_id, project_id),
		FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS schedule_overload_reminders (
		team_id BIGINT NOT NULL,
		user_id BIGINT NOT NULL,
		week TEXT NOT NULL,
		created_at TEXT NOT NULL,
		PRIMARY KEY (team_id, user_id, week)
	)`,
	`CREATE TABLE IF NOT EXISTS invoice_lines (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    invoice_id BIGINT NOT NULL,
	    label TEXT NOT NULL,
	    detail TEXT NOT NULL DEFAULT '',
	    seconds BIGINT NOT NULL DEFAULT 0,
	    rate_cents BIGINT NOT NULL DEFAULT 0,
	    amount_cents BIGINT NOT NULL DEFAULT 0,
	    FOREIGN KEY (invoice_id) REFERENCES invoices(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_invoice_lines_invoice ON invoice_lines(invoice_id)`,
	// Wave 4: audit log (enterprise).
	`CREATE TABLE IF NOT EXISTS audit_log (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    team_id BIGINT NOT NULL DEFAULT 0,
	    user_id BIGINT NOT NULL DEFAULT 0,
	    action TEXT NOT NULL,
	    target TEXT NOT NULL DEFAULT '',
	    meta TEXT NOT NULL DEFAULT '',
	    ip TEXT NOT NULL DEFAULT '',
	    created_at TEXT NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_audit_log_team ON audit_log(team_id, created_at)`,
	// Wave 4: outbound webhooks.
	`CREATE TABLE IF NOT EXISTS webhooks (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    team_id BIGINT NOT NULL,
	    url TEXT NOT NULL,
	    secret TEXT NOT NULL DEFAULT '',
	    events TEXT NOT NULL DEFAULT 'session.stopped,invoice.created',
	    active BIGINT NOT NULL DEFAULT 1,
	    created_at TEXT NOT NULL,
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_webhooks_team ON webhooks(team_id)`,
	`CREATE TABLE IF NOT EXISTS webhook_deliveries (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    webhook_id BIGINT NOT NULL,
	    event TEXT NOT NULL,
	    payload TEXT NOT NULL,
	    status BIGINT NOT NULL DEFAULT 0,
	    error TEXT NOT NULL DEFAULT '',
	    created_at TEXT NOT NULL,
	    FOREIGN KEY (webhook_id) REFERENCES webhooks(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_webhook_deliveries ON webhook_deliveries(webhook_id)`,
	`CREATE TABLE IF NOT EXISTS webhook_event_outbox (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    team_id BIGINT NOT NULL,
	    event TEXT NOT NULL,
	    payload TEXT NOT NULL,
	    -- Retained during rolling deploys; normalized targets are canonical.
	    webhook_ids TEXT NOT NULL DEFAULT '',
	    attempts BIGINT NOT NULL DEFAULT 0,
	    available_at TEXT NOT NULL,
	    locked_at TEXT,
	    created_at TEXT NOT NULL,
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS webhook_event_targets (
	    event_id BIGINT NOT NULL,
	    webhook_id BIGINT NOT NULL,
	    PRIMARY KEY (event_id, webhook_id),
	    FOREIGN KEY (event_id) REFERENCES webhook_event_outbox(id) ON DELETE CASCADE,
	    FOREIGN KEY (webhook_id) REFERENCES webhooks(id) ON DELETE CASCADE
	)`,
	`DO $$
	DECLARE
	    event_row RECORD;
	    target_id TEXT;
	BEGIN
	    IF EXISTS (
	        SELECT 1 FROM information_schema.columns
	        WHERE table_schema = current_schema()
	          AND table_name = 'webhook_event_outbox' AND column_name = 'webhook_ids'
	    ) THEN
	        FOR event_row IN EXECUTE 'SELECT id, webhook_ids FROM webhook_event_outbox' LOOP
	            FOREACH target_id IN ARRAY string_to_array(event_row.webhook_ids, ',') LOOP
	                IF target_id <> '' THEN
	                    INSERT INTO webhook_event_targets (event_id, webhook_id)
	                    SELECT event_row.id, target_id::BIGINT
	                    WHERE EXISTS (SELECT 1 FROM webhooks WHERE id = target_id::BIGINT)
	                    ON CONFLICT DO NOTHING;
	                END IF;
	            END LOOP;
	        END LOOP;
	    END IF;
	END $$`,
	`CREATE INDEX IF NOT EXISTS idx_webhook_event_claim ON webhook_event_outbox(available_at, id)`,
	`CREATE INDEX IF NOT EXISTS idx_webhook_event_targets_webhook ON webhook_event_targets(webhook_id)`,
	`CREATE TABLE IF NOT EXISTS webhook_delivery_queue (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    team_id BIGINT NOT NULL,
	    webhook_id BIGINT NOT NULL,
	    source_event_id BIGINT,
	    event TEXT NOT NULL,
	    payload TEXT NOT NULL,
	    attempts BIGINT NOT NULL DEFAULT 0,
	    available_at TEXT NOT NULL,
	    locked_at TEXT,
	    created_at TEXT NOT NULL,
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
	    FOREIGN KEY (webhook_id) REFERENCES webhooks(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_webhook_queue_claim ON webhook_delivery_queue(available_at, id)`,
	`CREATE INDEX IF NOT EXISTS idx_webhook_queue_webhook ON webhook_delivery_queue(webhook_id)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_webhook_queue_source ON webhook_delivery_queue(source_event_id, webhook_id) WHERE source_event_id IS NOT NULL`,
	// CRM (platform-crm-calendar-mcp): contacts of a client, deals, the activity
	// timeline, and the team calendar. A deal's stage is a key of its workspace's
	// crm_stages rows, so the table carries no list of stages. Times use the same
	// text format as the rest of the schema; money is in cents.
	`CREATE TABLE IF NOT EXISTS crm_contacts (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    team_id BIGINT NOT NULL,
	    client_id BIGINT NOT NULL,
	    name TEXT NOT NULL,
	    role TEXT NOT NULL DEFAULT '',
	    email TEXT NOT NULL DEFAULT '',
	    phone TEXT NOT NULL DEFAULT '',
	    created_by BIGINT NOT NULL DEFAULT 0,
	    created_at TEXT NOT NULL,
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
	    FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_crm_contacts_client ON crm_contacts(team_id, client_id)`,
	`CREATE TABLE IF NOT EXISTS crm_deals (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    team_id BIGINT NOT NULL,
	    title TEXT NOT NULL,
	    stage TEXT NOT NULL,
	    client_id BIGINT,
	    contact_id BIGINT,
	    owner_id BIGINT NOT NULL,
	    amount_cents BIGINT NOT NULL DEFAULT 0,
	    currency TEXT NOT NULL DEFAULT '',
	    probability BIGINT NOT NULL DEFAULT 0 CHECK (probability BETWEEN 0 AND 100),
	    close_date TEXT NOT NULL DEFAULT '',
	    next_step_at TEXT NOT NULL DEFAULT '',
	    lost_reason TEXT NOT NULL DEFAULT '',
	    created_by BIGINT NOT NULL DEFAULT 0,
	    created_at TEXT NOT NULL,
	    updated_at TEXT NOT NULL,
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
	    FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE SET NULL,
	    FOREIGN KEY (contact_id) REFERENCES crm_contacts(id) ON DELETE SET NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_crm_deals_stage ON crm_deals(team_id, stage)`,
	`CREATE INDEX IF NOT EXISTS idx_crm_deals_owner ON crm_deals(team_id, owner_id)`,
	// A workspace's pipeline. Its rows appear on the first write to its stages; until
	// then a read sees model.DefaultDealStages. Deals keep their keys, so no deal row
	// changes here.
	`CREATE TABLE IF NOT EXISTS crm_stages (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    team_id BIGINT NOT NULL,
	    key TEXT NOT NULL,
	    name TEXT NOT NULL,
	    position BIGINT NOT NULL,
	    kind TEXT NOT NULL CHECK (kind IN ('open', 'won', 'lost')),
	    created_at TEXT NOT NULL,
	    UNIQUE (team_id, key),
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_crm_stages_position ON crm_stages(team_id, position)`,
	// The fixed stage list was an inline CHECK, named after its column by default.
	// Databases that already have it lose it here. The catalog is read first, so a
	// database that has dropped it takes no table lock on startup.
	`DO $$ BEGIN
	   IF EXISTS (
	     SELECT 1 FROM pg_constraint
	     WHERE conrelid = 'crm_deals'::regclass
	       AND conname = 'crm_deals_stage_check'
	   ) THEN
	     ALTER TABLE crm_deals DROP CONSTRAINT crm_deals_stage_check;
	   END IF;
	 END $$`,
	`CREATE TABLE IF NOT EXISTS crm_activities (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    team_id BIGINT NOT NULL,
	    client_id BIGINT,
	    deal_id BIGINT,
	    kind TEXT NOT NULL CHECK (kind IN ('call', 'meeting', 'note')),
	    body TEXT NOT NULL DEFAULT '',
	    result TEXT NOT NULL DEFAULT '',
	    occurred_at TEXT NOT NULL,
	    author_id BIGINT NOT NULL,
	    created_at TEXT NOT NULL,
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
	    FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE,
	    FOREIGN KEY (deal_id) REFERENCES crm_deals(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_crm_activities_client ON crm_activities(team_id, client_id)`,
	`CREATE INDEX IF NOT EXISTS idx_crm_activities_deal ON crm_activities(team_id, deal_id)`,
	// Leads (CRM-14): an enquiry before it is a client and a deal. A lead converts once and
	// keeps the ids it made. Owner 0 is an enquiry from the website that nobody has taken.
	// The intake table holds one key per workspace, as its hash only; the key is the
	// primary key, so the route finds the workspace with one lookup.
	`CREATE TABLE IF NOT EXISTS crm_leads (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    team_id BIGINT NOT NULL,
	    company TEXT NOT NULL DEFAULT '',
	    contact_name TEXT NOT NULL DEFAULT '',
	    phone TEXT NOT NULL DEFAULT '',
	    email TEXT NOT NULL DEFAULT '',
	    source TEXT NOT NULL DEFAULT '',
	    message TEXT NOT NULL DEFAULT '',
	    owner_id BIGINT NOT NULL DEFAULT 0,
	    status TEXT NOT NULL DEFAULT 'new' CHECK (status IN ('new', 'contacted', 'qualified', 'lost', 'converted')),
	    lost_reason TEXT NOT NULL DEFAULT '',
	    next_step_at TEXT NOT NULL DEFAULT '',
	    converted_deal_id BIGINT,
	    converted_client_id BIGINT,
	    created_by BIGINT NOT NULL DEFAULT 0,
	    created_at TEXT NOT NULL,
	    updated_at TEXT NOT NULL,
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
	    FOREIGN KEY (converted_deal_id) REFERENCES crm_deals(id) ON DELETE SET NULL,
	    FOREIGN KEY (converted_client_id) REFERENCES clients(id) ON DELETE SET NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_crm_leads_status ON crm_leads(team_id, status)`,
	`CREATE TABLE IF NOT EXISTS crm_intake_keys (
	    key_hash TEXT PRIMARY KEY,
	    team_id BIGINT NOT NULL UNIQUE,
	    created_by BIGINT NOT NULL DEFAULT 0,
	    created_at TEXT NOT NULL,
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE
	)`,
	// An activity may belong to a lead instead of a client or a deal. It is added after
	// crm_leads exists, because the reference needs the table.
	`ALTER TABLE crm_activities ADD COLUMN IF NOT EXISTS lead_id BIGINT REFERENCES crm_leads(id) ON DELETE CASCADE`,
	`CREATE INDEX IF NOT EXISTS idx_crm_activities_lead ON crm_activities(team_id, lead_id)`,
	// One next-step reminder per item, channel and workspace day (CRM-15). The claim is
	// the primary key, so a second pass the same day finds it and sends nothing.
	`CREATE TABLE IF NOT EXISTS crm_reminders (
	    team_id BIGINT NOT NULL,
	    item_kind TEXT NOT NULL CHECK (item_kind IN ('deal', 'lead')),
	    item_id BIGINT NOT NULL,
	    day TEXT NOT NULL,
	    channel TEXT NOT NULL CHECK (channel IN ('push', 'email')),
	    sent_at TEXT NOT NULL,
	    PRIMARY KEY (team_id, item_kind, item_id, day, channel),
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS calendar_events (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    team_id BIGINT NOT NULL,
	    title TEXT NOT NULL,
	    starts_at TEXT NOT NULL,
	    ends_at TEXT NOT NULL,
	    location TEXT NOT NULL DEFAULT '',
	    notes TEXT NOT NULL DEFAULT '',
	    project_id BIGINT,
	    client_id BIGINT,
	    shared BIGINT NOT NULL DEFAULT 0,
	    created_by BIGINT NOT NULL,
	    created_at TEXT NOT NULL,
	    updated_at TEXT NOT NULL,
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
	    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL,
	    FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE SET NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_calendar_events_period ON calendar_events(team_id, starts_at)`,
	`CREATE TABLE IF NOT EXISTS calendar_event_participants (
	    event_id BIGINT NOT NULL,
	    user_id BIGINT NOT NULL,
	    PRIMARY KEY (event_id, user_id),
	    FOREIGN KEY (event_id) REFERENCES calendar_events(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_calendar_participants_user ON calendar_event_participants(user_id)`,
	// The confirmation queue (agent-action-queue). An agent only requests one of these
	// four actions; a person confirms or rejects it in the app. A claimed confirmation
	// keeps status pending with decided_at set until its outcome is written.
	`CREATE TABLE IF NOT EXISTS agent_actions (
	    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
	    team_id BIGINT NOT NULL,
	    requested_by BIGINT NOT NULL,
	    kind TEXT NOT NULL CHECK (kind IN ('invoice_delete', 'payroll_run_delete', 'week_reopen', 'workspace_delete')),
	    target_id BIGINT NOT NULL DEFAULT 0,
	    target_label TEXT NOT NULL DEFAULT '',
	    reason TEXT NOT NULL DEFAULT '',
	    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'done', 'failed', 'rejected')),
	    decided_by BIGINT NOT NULL DEFAULT 0,
	    decided_at TEXT,
	    error TEXT NOT NULL DEFAULT '',
	    created_at TEXT NOT NULL,
	    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS idx_agent_actions_status ON agent_actions(team_id, status)`,
}
