package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// ResolveWorkspaceEmails maps each email to the account that has it, ignoring
// case. The emails no account has come back as missing: a restore must not start
// while any are there, because the people in a workspace are matched by email.
func (d *DB) ResolveWorkspaceEmails(ctx context.Context, emails []string) (map[string]int64, []string, error) {
	found := map[string]int64{}
	if len(emails) == 0 {
		return found, nil, nil
	}
	err := queryRows(ctx, d.sql, "find people in the file",
		`SELECT id, lower(email::text) FROM users WHERE lower(email::text) = ANY(?::text[])`,
		func(rows *sql.Rows) error {
			var id int64
			var email string
			if err := rows.Scan(&id, &email); err != nil {
				return err
			}
			found[email] = id
			return nil
		}, emails)
	if err != nil {
		return nil, nil, err
	}
	var missing []string
	for _, email := range emails {
		if _, ok := found[strings.ToLower(email)]; !ok {
			missing = append(missing, email)
		}
	}
	return found, missing, nil
}

// RestoreWorkspace creates a workspace the caller owns and writes the file into it
// through one transaction. Any error from the write rolls the whole restore back,
// so a refused or cut-off file leaves nothing behind.
func (d *DB) RestoreWorkspace(ctx context.Context, request appmodel.WorkspaceRestoreRequest) (appmodel.WorkspaceRestoreResult, error) {
	if request.CallerID <= 0 || request.Write == nil {
		return appmodel.WorkspaceRestoreResult{}, ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return appmodel.WorkspaceRestoreResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	r := &restorer{
		ctx: ctx, tx: tx, callerID: request.CallerID, users: request.Users, now: FormatTime(d.currentTime().UTC()), seal: d.sealSecret,
		clients: map[int64]int64{}, projects: map[int64]int64{}, tags: map[int64]int64{},
		activities: map[int64]int64{}, tasks: map[int64]int64{}, invoices: map[int64]int64{},
	}
	if err := request.Write(r); err != nil {
		return appmodel.WorkspaceRestoreResult{}, err
	}
	if r.teamID == 0 {
		return appmodel.WorkspaceRestoreResult{}, fmt.Errorf("%w: the file holds no workspace", appmodel.ErrUnsupportedWorkspaceFile)
	}
	if !r.ownerSet {
		if err := r.exec("owner", `INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'owner', ?)`,
			r.teamID, r.callerID, r.now); err != nil {
			return appmodel.WorkspaceRestoreResult{}, err
		}
	}
	// The restored workspace opens its own trail with this row, so a restore never
	// starts from a blank log.
	var members, invoices int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memberships WHERE team_id = ?`, r.teamID).Scan(&members); err != nil {
		return appmodel.WorkspaceRestoreResult{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM invoices WHERE team_id = ?`, r.teamID).Scan(&invoices); err != nil {
		return appmodel.WorkspaceRestoreResult{}, err
	}
	if err := auditInTx(ctx, tx, r.teamID, r.callerID, "workspace.restore", fmt.Sprintf("team:%d", r.teamID),
		fmt.Sprintf("участников: %d, счетов: %d", members, invoices), r.now); err != nil {
		return appmodel.WorkspaceRestoreResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return appmodel.WorkspaceRestoreResult{}, err
	}
	return appmodel.WorkspaceRestoreResult{TeamID: r.teamID, Slug: r.slug, Webhooks: r.webhooks}, nil
}

// restorer writes one export as its records arrive. Each record refers only to
// records that came before it, so every id resolves at once.
type restorer struct {
	ctx        context.Context
	tx         *Tx
	seal       func(string) (string, error)
	teamID     int64
	slug       string
	callerID   int64
	ownerSet   bool
	now        string
	users      map[string]int64
	clients    map[int64]int64
	projects   map[int64]int64
	tags       map[int64]int64
	activities map[int64]int64
	tasks      map[int64]int64
	invoices   map[int64]int64
	webhooks   []appmodel.RestoredWebhook
}

// mapped turns an id from the file into the restored id. Zero means none.
func mapped(ids map[int64]int64, old int64, what string) (any, error) {
	if old == 0 {
		return nil, nil
	}
	id, ok := ids[old]
	if !ok {
		return nil, fmt.Errorf("the file refers to %s %d, which it does not hold", what, old)
	}
	return id, nil
}

// person turns an email from the file into a user id. An empty email is nobody.
func (r *restorer) person(email string) (any, error) {
	if email == "" {
		return nil, nil
	}
	id, ok := r.users[strings.ToLower(email)]
	if !ok {
		return nil, fmt.Errorf("the file names %s, who is not registered", email)
	}
	return id, nil
}

// recorder is the person a record is credited to when the file does not say who
// it was: the caller doing the restore.
func (r *restorer) recorder(email string) (any, error) {
	id, err := r.person(email)
	if err != nil || id != nil {
		return id, err
	}
	return r.callerID, nil
}

func (r *restorer) exec(what string, query string, args ...any) error {
	if _, err := r.tx.ExecContext(r.ctx, query, args...); err != nil {
		return fmt.Errorf("restore %s: %w", what, err)
	}
	return nil
}

func (r *restorer) insertID(what string, query string, args ...any) (int64, error) {
	var id int64
	if err := r.tx.QueryRowContext(r.ctx, query, args...).Scan(&id); err != nil {
		return 0, fmt.Errorf("restore %s: %w", what, err)
	}
	return id, nil
}

// freeTeamSlug returns the slug unless another workspace has it, then the first
// of slug-2, slug-3, ... that is free.
func freeTeamSlug(ctx context.Context, tx *Tx, slug string) (string, error) {
	base := strings.TrimSpace(slug)
	if base == "" {
		base = "workspace"
	}
	for i := 1; ; i++ {
		candidate := base
		if i > 1 {
			candidate = fmt.Sprintf("%s-%d", base, i)
		}
		var taken int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM teams WHERE slug = ?`, candidate).Scan(&taken); err != nil {
			return "", err
		}
		if taken == 0 {
			return candidate, nil
		}
	}
}

// Header has nothing to write: the reader checked the format before the first record.
func (r *restorer) Header(time.Time) error { return nil }

// Team creates the new workspace under a free slug, owned by the caller.
func (r *restorer) Team(t appmodel.ExportTeam) error {
	slug, err := freeTeamSlug(r.ctx, r.tx, t.Slug)
	if err != nil {
		return err
	}
	id, err := r.insertID("workspace",
		`INSERT INTO teams (slug, name, owner_id, created_at, currency, round_minutes, round_mode, invoice_prefix,
		                    requisites, vat_note, modules, logo, sections_chosen)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1) RETURNING id`,
		slug, t.Name, r.callerID, r.now, t.Currency, t.RoundMinutes, t.RoundMode, t.InvoicePrefix, t.Requisites, t.VATNote, t.Modules, t.Logo)
	if err != nil {
		return err
	}
	r.teamID, r.slug = id, slug
	return nil
}

// Member adds one person with the role and rates the file gave them. A second
// owner in the file becomes an admin: a workspace has one owner, the caller. The
// caller's own row keeps the rates from the file.
func (r *restorer) Member(m appmodel.ExportMember) error {
	userID := r.users[strings.ToLower(m.Email)]
	if userID == r.callerID {
		r.ownerSet = true
		return r.exec("owner settings",
			`INSERT INTO memberships (team_id, user_id, role, joined_at, hourly_pay_cents, capacity_minutes, admin_minutes)
			 VALUES (?, ?, 'owner', ?, ?, ?, ?)`,
			r.teamID, r.callerID, m.JoinedAt, m.HourlyPayCents, m.CapacityMinutes, m.AdminMinutes)
	}
	role := m.Role
	if role == "owner" {
		role = "admin"
	}
	return r.exec("member "+m.Email,
		`INSERT INTO memberships (team_id, user_id, role, joined_at, hourly_pay_cents, capacity_minutes, admin_minutes)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.teamID, userID, role, m.JoinedAt, m.HourlyPayCents, m.CapacityMinutes, m.AdminMinutes)
}

func (r *restorer) Client(c appmodel.ExportClient) error {
	id, err := r.insertID("client "+c.Name,
		`INSERT INTO clients (team_id, name, email, details, currency, notes, archived, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		r.teamID, c.Name, c.Email, c.Details, c.Currency, c.Notes, boolInt(c.Archived), c.CreatedAt, c.UpdatedAt)
	if err != nil {
		return err
	}
	r.clients[c.ID] = id
	return nil
}

func (r *restorer) Project(p appmodel.ExportProject) error {
	clientID, err := mapped(r.clients, p.ClientID, "client")
	if err != nil {
		return err
	}
	lead, err := r.person(p.LeadEmail)
	if err != nil {
		return err
	}
	id, err := r.insertID("project "+p.Name,
		`INSERT INTO projects (team_id, slug, name, color, archived, created_at, updated_at, estimate_minutes, deadline,
		                       lead_user_id, billable_rate_cents, billable, currency, client_id, budget_cents,
		                       client_name, client_details, client_email)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		r.teamID, p.Slug, p.Name, p.Color, boolInt(p.Archived), p.CreatedAt, p.UpdatedAt, nullIfZero(p.EstimateMinutes),
		nullIfEmpty(p.Deadline), lead, nullIfZero(p.BillableRateCents), boolInt(p.Billable), p.Currency, clientID,
		nullIfZero(p.BudgetCents), p.ClientName, p.ClientDetails, p.ClientEmail)
	if err != nil {
		return err
	}
	r.projects[p.ID] = id
	return nil
}

func (r *restorer) Tag(t appmodel.ExportTag) error {
	id, err := r.insertID("tag "+t.Name,
		`INSERT INTO tags (name, team_id, created_at) VALUES (?, ?, ?) RETURNING id`, t.Name, r.teamID, r.now)
	if err != nil {
		return err
	}
	r.tags[t.ID] = id
	return nil
}

func (r *restorer) Task(t appmodel.ExportTask) error {
	projectID, err := mapped(r.projects, t.ProjectID, "project")
	if err != nil {
		return err
	}
	assignee, err := r.person(t.AssigneeEmail)
	if err != nil {
		return err
	}
	creator, err := r.recorder(t.CreatedByEmail)
	if err != nil {
		return err
	}
	id, err := r.insertID("task "+t.Title,
		`INSERT INTO tasks (team_id, project_id, title, status, position, assignee_user_id, due_date, estimate_minutes,
		                    created_by, archived, created_at, updated_at, external_ref, external_url)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		r.teamID, projectID, t.Title, t.Status, t.Position, assignee, nullIfEmpty(t.DueDate), nullIfZero(int64(t.EstimateMinutes)),
		creator, boolInt(t.Archived), t.CreatedAt, t.UpdatedAt, nullIfEmpty(t.ExternalRef), nullIfEmpty(t.ExternalURL))
	if err != nil {
		return err
	}
	r.tasks[t.ID] = id
	return nil
}

func (r *restorer) Activity(a appmodel.ExportActivity) error {
	projectID, err := mapped(r.projects, a.ProjectID, "project")
	if err != nil {
		return err
	}
	taskID, err := mapped(r.tasks, a.TaskID, "task")
	if err != nil {
		return err
	}
	id, err := r.insertID("activity "+a.Name,
		`INSERT INTO activities (name, team_id, project_id, task_id, archived, color, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		a.Name, r.teamID, projectID, taskID, boolInt(a.Archived), a.Color, r.now, r.now)
	if err != nil {
		return err
	}
	r.activities[a.ID] = id
	return nil
}

func (r *restorer) Goal(g appmodel.ExportGoal) error {
	activityID, err := mapped(r.activities, g.ActivityID, "activity")
	if err != nil {
		return err
	}
	return r.exec("goal", `INSERT INTO goals (activity_id, team_id, period, target_minutes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`, activityID, r.teamID, g.Period, g.TargetMinutes, r.now, r.now)
}

func (r *restorer) Invoice(inv appmodel.ExportInvoice) error {
	projectID, err := mapped(r.projects, inv.ProjectID, "project")
	if err != nil {
		return err
	}
	id, err := r.insertID("invoice "+inv.Number,
		`INSERT INTO invoices (team_id, number, client_name, client_email, client_details, project_id, period_start, period_end,
		                       status, notes, receipt, paid_at, currency, seller_details, vat_note, by_person, revision, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		r.teamID, inv.Number, inv.ClientName, inv.ClientEmail, inv.ClientDetails, projectID, inv.PeriodStart, inv.PeriodEnd,
		inv.Status, inv.Notes, inv.Receipt, nullIfEmpty(inv.PaidAt), inv.Currency, inv.SellerDetails, inv.VATNote,
		boolInt(inv.ByPerson), inv.Revision, inv.CreatedAt)
	if err != nil {
		return err
	}
	r.invoices[inv.ID] = id
	for _, line := range inv.Lines {
		if err := r.exec("invoice line of "+inv.Number,
			`INSERT INTO invoice_lines (invoice_id, label, detail, seconds, rate_cents, amount_cents) VALUES (?, ?, ?, ?, ?, ?)`,
			id, line.Label, line.Detail, line.Seconds, line.RateCents, line.AmountCents); err != nil {
			return err
		}
	}
	return nil
}

func (r *restorer) PayrollRun(run appmodel.ExportPayrollRun) error {
	id, err := r.insertID("payroll run "+run.Number,
		`INSERT INTO payroll_runs (team_id, number, period_start, period_end, status, notes, created_at, currency)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		r.teamID, run.Number, run.PeriodStart, run.PeriodEnd, run.Status, run.Notes, run.CreatedAt, run.Currency)
	if err != nil {
		return err
	}
	for _, line := range run.Lines {
		userID, err := r.person(line.UserEmail)
		if err != nil {
			return err
		}
		if err := r.exec("payroll line of "+run.Number,
			`INSERT INTO payroll_lines (run_id, user_id, label, seconds, rate_cents, amount_cents) VALUES (?, ?, ?, ?, ?, ?)`,
			id, userID, line.Label, line.Seconds, line.RateCents, line.AmountCents); err != nil {
			return err
		}
	}
	return nil
}

// Session restores finished time with its tags and its invoice. The invoice comes
// before the sessions billed to it in the file.
func (r *restorer) Session(s appmodel.ExportSession) error {
	activityID, err := mapped(r.activities, s.ActivityID, "activity")
	if err != nil {
		return err
	}
	if activityID == nil {
		return fmt.Errorf("a session has no activity in the file")
	}
	userID, err := r.person(s.UserEmail)
	if err != nil {
		return err
	}
	invoiceID, err := mapped(r.invoices, s.InvoiceID, "invoice")
	if err != nil {
		return err
	}
	var end any
	if s.EndAt != nil {
		end = *s.EndAt
	}
	id, err := r.insertID("session",
		`INSERT INTO sessions (activity_id, team_id, start_at, end_at, note, paused, accumulated_seconds, user_id,
		                       external_id, created_at, updated_at, invoice_id)
		 VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?) RETURNING id`,
		activityID, r.teamID, s.StartAt, end, nullableString(s.Note), s.AccumulatedSeconds, userID,
		nullIfEmpty(s.ExternalID), s.CreatedAt, s.UpdatedAt, invoiceID)
	if err != nil {
		return err
	}
	for _, tagID := range s.TagIDs {
		tag, err := mapped(r.tags, tagID, "tag")
		if err != nil {
			return err
		}
		if err := r.exec("session tag", `INSERT INTO session_tags (session_id, tag_id) VALUES (?, ?)`, id, tag); err != nil {
			return err
		}
	}
	return nil
}

func (r *restorer) Schedule(e appmodel.ExportSchedule) error {
	userID, err := r.person(e.UserEmail)
	if err != nil {
		return err
	}
	projectID, err := mapped(r.projects, e.ProjectID, "project")
	if err != nil {
		return err
	}
	return r.exec("schedule entry", `INSERT INTO schedule_entries (team_id, user_id, project_id, day, minutes, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, r.teamID, userID, projectID, e.Day, e.Minutes, e.Note, e.CreatedAt)
}

func (r *restorer) ScheduleRow(l appmodel.ExportScheduleRow) error {
	userID, err := r.person(l.UserEmail)
	if err != nil {
		return err
	}
	projectID, err := mapped(r.projects, l.ProjectID, "project")
	if err != nil {
		return err
	}
	return r.exec("schedule line", `INSERT INTO schedule_lines (team_id, user_id, project_id, created_at) VALUES (?, ?, ?, ?)`,
		r.teamID, userID, projectID, l.CreatedAt)
}

func (r *restorer) Approval(a appmodel.ExportApproval) error {
	approver, err := r.recorder(a.ApprovedByEmail)
	if err != nil {
		return err
	}
	return r.exec("approved week", `INSERT INTO timesheet_approvals (team_id, starts_at, ends_at, approved_by, approved_at)
		VALUES (?, ?, ?, ?, ?)`, r.teamID, a.StartsAt, a.EndsAt, approver, a.ApprovedAt)
}

func (r *restorer) Report(rep appmodel.ExportReport) error {
	creator, err := r.recorder(rep.CreatedByEmail)
	if err != nil {
		return err
	}
	projectID, err := mapped(r.projects, rep.ProjectID, "project")
	if err != nil {
		return err
	}
	tagID, err := mapped(r.tags, rep.TagID, "tag")
	if err != nil {
		return err
	}
	return r.exec("saved report "+rep.Name, `INSERT INTO saved_reports (team_id, name, period, project_slug, tag, created_by,
		created_at, project_id, tag_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.teamID, rep.Name, rep.Period, rep.ProjectSlug, rep.Tag, creator, rep.CreatedAt, projectID, tagID)
}

// Webhook recreates an endpoint with a new signing secret. The file never holds
// the old one, so the secret returned here is the only copy the owner sees.
// The old endpoint, in the old workspace, keeps sending until it is deleted.
func (r *restorer) Webhook(w appmodel.ExportWebhook) error {
	url := strings.TrimSpace(w.URL)
	if url == "" || (!strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://")) {
		return fmt.Errorf("%w: a webhook address is not http(s)", appmodel.ErrUnsupportedWorkspaceFile)
	}
	events := strings.TrimSpace(w.Events)
	if events == "" {
		events = defaultWebhookEvents
	}
	secret, err := newWebhookSecret()
	if err != nil {
		return err
	}
	sealed, err := r.seal(secret)
	if err != nil {
		return fmt.Errorf("seal webhook secret: %w", err)
	}
	if err := r.exec("webhook", `INSERT INTO webhooks (team_id, url, secret, events, active, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, r.teamID, url, sealed, events, webhookFlag(w.Active), r.now); err != nil {
		return err
	}
	r.webhooks = append(r.webhooks, appmodel.RestoredWebhook{URL: url, Secret: secret})
	return nil
}

// Integration is not restored: a connection names an outside account, and the
// owner connects it again from the integrations page.
func (r *restorer) Integration(appmodel.ExportIntegration) error { return nil }

// newWebhookSecret makes the signing secret a restored endpoint gets.
func newWebhookSecret() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
