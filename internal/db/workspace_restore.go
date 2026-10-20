package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// RestoreWorkspace creates a new workspace from an export, owned by the caller.
// People are matched by email. An email the export names but this installation
// does not know stops the restore before anything is written, and the error
// lists every such email. Ids in the file are mapped to new ids; a reference to
// something the file does not hold is an error, never a silent blank. The whole
// restore is one transaction.
func (d *DB) RestoreWorkspace(ctx context.Context, request appmodel.WorkspaceRestoreRequest) (appmodel.WorkspaceRestoreResult, error) {
	var out appmodel.WorkspaceRestoreResult
	export := request.Export
	if request.CallerID <= 0 || export.Format != appmodel.WorkspaceExportFormat || export.Version != appmodel.WorkspaceExportVersion {
		return out, ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()

	users, err := resolveExportEmails(ctx, tx, export)
	if err != nil {
		return out, err
	}
	slug, err := freeTeamSlug(ctx, tx, export.Team.Slug)
	if err != nil {
		return out, err
	}
	r := &restorer{
		ctx: ctx, tx: tx, callerID: request.CallerID, users: users, now: FormatTime(d.currentTime().UTC()),
		clients: map[int64]int64{}, projects: map[int64]int64{}, tags: map[int64]int64{},
		activities: map[int64]int64{}, tasks: map[int64]int64{}, sessions: map[int64]int64{}, invoices: map[int64]int64{},
	}
	if err := r.team(slug, export.Team); err != nil {
		return out, err
	}
	if err := r.members(export.Members); err != nil {
		return out, err
	}
	if err := r.clientsProjectsTags(export); err != nil {
		return out, err
	}
	if err := r.activitiesAndTasks(export); err != nil {
		return out, err
	}
	if err := r.money(export); err != nil {
		return out, err
	}
	if err := r.sessionsAndPlans(export); err != nil {
		return out, err
	}
	if err := r.planning(export); err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return out, err
	}
	out.TeamID, out.Slug = r.teamID, slug
	return out, nil
}

// restorer carries one restore's transaction and its id maps.
type restorer struct {
	ctx        context.Context
	tx         *Tx
	teamID     int64
	callerID   int64
	now        string
	users      map[string]int64
	clients    map[int64]int64
	projects   map[int64]int64
	tags       map[int64]int64
	activities map[int64]int64
	tasks      map[int64]int64
	sessions   map[int64]int64
	invoices   map[int64]int64
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

// resolveExportEmails maps every email the file uses to a user id, or refuses
// with the list of emails that have no account.
func resolveExportEmails(ctx context.Context, tx *Tx, export appmodel.WorkspaceExport) (map[string]int64, error) {
	wanted := map[string]bool{}
	add := func(email string) {
		if email != "" {
			wanted[strings.ToLower(email)] = true
		}
	}
	for _, m := range export.Members {
		add(m.Email)
	}
	for _, p := range export.Projects {
		add(p.LeadEmail)
	}
	for _, s := range export.Sessions {
		add(s.UserEmail)
	}
	for _, t := range export.Tasks {
		add(t.AssigneeEmail)
		add(t.CreatedByEmail)
	}
	for _, run := range export.PayrollRuns {
		for _, l := range run.Lines {
			add(l.UserEmail)
		}
	}
	for _, e := range export.ScheduleEntries {
		add(e.UserEmail)
	}
	for _, l := range export.ScheduleLines {
		add(l.UserEmail)
	}
	for _, a := range export.TimesheetApprovals {
		add(a.ApprovedByEmail)
	}
	for _, rep := range export.SavedReports {
		add(rep.CreatedByEmail)
	}
	emails := make([]string, 0, len(wanted))
	for email := range wanted {
		emails = append(emails, email)
	}
	found := map[string]int64{}
	if len(emails) == 0 {
		return found, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, lower(email::text) FROM users WHERE lower(email::text) = ANY(?::text[])`, emails)
	if err != nil {
		return nil, fmt.Errorf("find people in the file: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var email string
		if err := rows.Scan(&id, &email); err != nil {
			return nil, err
		}
		found[email] = id
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var missing []string
	for _, email := range emails {
		if _, ok := found[email]; !ok {
			missing = append(missing, email)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: %s", appmodel.ErrRestorePeopleMissing, strings.Join(missing, ", "))
	}
	return found, nil
}

func (r *restorer) team(slug string, t appmodel.ExportTeam) error {
	id, err := r.insertID("workspace",
		`INSERT INTO teams (slug, name, owner_id, created_at, currency, round_minutes, round_mode, invoice_prefix,
		                    requisites, vat_note, modules, logo, sections_chosen)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1) RETURNING id`,
		slug, t.Name, r.callerID, r.now, t.Currency, t.RoundMinutes, t.RoundMode, t.InvoicePrefix, t.Requisites, t.VATNote, t.Modules, t.Logo)
	r.teamID = id
	return err
}

// members adds the caller as owner, then everyone in the file with the role they
// had. A second owner in the file becomes an admin: a workspace has one owner.
func (r *restorer) members(members []appmodel.ExportMember) error {
	for _, m := range members {
		userID := r.users[strings.ToLower(m.Email)]
		role := m.Role
		if userID == r.callerID {
			continue
		}
		if role == "owner" {
			role = "admin"
		}
		if err := r.exec("member "+m.Email,
			`INSERT INTO memberships (team_id, user_id, role, joined_at, hourly_pay_cents, capacity_minutes, admin_minutes)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			r.teamID, userID, role, m.JoinedAt, m.HourlyPayCents, m.CapacityMinutes, m.AdminMinutes); err != nil {
			return err
		}
	}
	// The caller's row keeps the rate and capacity from the file when the caller
	// is in it, so the owner's own numbers survive the move.
	for _, m := range members {
		if r.users[strings.ToLower(m.Email)] == r.callerID {
			return r.exec("owner settings",
				`INSERT INTO memberships (team_id, user_id, role, joined_at, hourly_pay_cents, capacity_minutes, admin_minutes)
				 VALUES (?, ?, 'owner', ?, ?, ?, ?)`,
				r.teamID, r.callerID, m.JoinedAt, m.HourlyPayCents, m.CapacityMinutes, m.AdminMinutes)
		}
	}
	return r.exec("owner", `INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'owner', ?)`,
		r.teamID, r.callerID, r.now)
}

func (r *restorer) clientsProjectsTags(export appmodel.WorkspaceExport) error {
	for _, c := range export.Clients {
		id, err := r.insertID("client "+c.Name,
			`INSERT INTO clients (team_id, name, email, details, currency, notes, archived, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			r.teamID, c.Name, c.Email, c.Details, c.Currency, c.Notes, boolInt(c.Archived), c.CreatedAt, c.UpdatedAt)
		if err != nil {
			return err
		}
		r.clients[c.ID] = id
	}
	for _, p := range export.Projects {
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
	}
	for _, t := range export.Tags {
		id, err := r.insertID("tag "+t.Name,
			`INSERT INTO tags (name, team_id, created_at) VALUES (?, ?, ?) RETURNING id`, t.Name, r.teamID, r.now)
		if err != nil {
			return err
		}
		r.tags[t.ID] = id
	}
	return nil
}

func (r *restorer) activitiesAndTasks(export appmodel.WorkspaceExport) error {
	for _, a := range export.Activities {
		projectID, err := mapped(r.projects, a.ProjectID, "project")
		if err != nil {
			return err
		}
		id, err := r.insertID("activity "+a.Name,
			`INSERT INTO activities (name, team_id, project_id, archived, color, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			a.Name, r.teamID, projectID, boolInt(a.Archived), a.Color, r.now, r.now)
		if err != nil {
			return err
		}
		r.activities[a.ID] = id
	}
	for _, t := range export.Tasks {
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
	}
	for _, a := range export.Activities {
		if a.TaskID == 0 {
			continue
		}
		taskID, err := mapped(r.tasks, a.TaskID, "task")
		if err != nil {
			return err
		}
		if err := r.exec("activity link "+a.Name, `UPDATE activities SET task_id = ? WHERE id = ?`, taskID, r.activities[a.ID]); err != nil {
			return err
		}
	}
	for _, g := range export.Goals {
		activityID, err := mapped(r.activities, g.ActivityID, "activity")
		if err != nil {
			return err
		}
		if err := r.exec("goal", `INSERT INTO goals (activity_id, team_id, period, target_minutes, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)`, activityID, r.teamID, g.Period, g.TargetMinutes, r.now, r.now); err != nil {
			return err
		}
	}
	return nil
}

func (r *restorer) money(export appmodel.WorkspaceExport) error {
	for _, inv := range export.Invoices {
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
	}
	for _, run := range export.PayrollRuns {
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
	}
	return nil
}

// sessionsAndPlans restores the finished time with its tags, then links the
// sessions that were billed to their restored invoices.
func (r *restorer) sessionsAndPlans(export appmodel.WorkspaceExport) error {
	for _, s := range export.Sessions {
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
		var end any
		if s.EndAt != nil {
			end = *s.EndAt
		}
		id, err := r.insertID("session",
			`INSERT INTO sessions (activity_id, team_id, start_at, end_at, note, paused, accumulated_seconds, user_id,
			                       external_id, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?) RETURNING id`,
			activityID, r.teamID, s.StartAt, end, nullableString(s.Note), s.AccumulatedSeconds, userID,
			nullIfEmpty(s.ExternalID), s.CreatedAt, s.UpdatedAt)
		if err != nil {
			return err
		}
		r.sessions[s.ID] = id
		for _, tagID := range s.TagIDs {
			tag, err := mapped(r.tags, tagID, "tag")
			if err != nil {
				return err
			}
			if err := r.exec("session tag", `INSERT INTO session_tags (session_id, tag_id) VALUES (?, ?)`, id, tag); err != nil {
				return err
			}
		}
	}
	for _, s := range export.Sessions {
		if s.InvoiceID == 0 {
			continue
		}
		invoiceID, err := mapped(r.invoices, s.InvoiceID, "invoice")
		if err != nil {
			return err
		}
		if err := r.exec("session invoice", `UPDATE sessions SET invoice_id = ? WHERE id = ?`, invoiceID, r.sessions[s.ID]); err != nil {
			return err
		}
	}
	return nil
}

// planning restores the schedule, the approved weeks and the saved reports.
func (r *restorer) planning(export appmodel.WorkspaceExport) error {
	for _, e := range export.ScheduleEntries {
		userID, err := r.person(e.UserEmail)
		if err != nil {
			return err
		}
		projectID, err := mapped(r.projects, e.ProjectID, "project")
		if err != nil {
			return err
		}
		if err := r.exec("schedule entry", `INSERT INTO schedule_entries (team_id, user_id, project_id, day, minutes, note, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, r.teamID, userID, projectID, e.Day, e.Minutes, e.Note, e.CreatedAt); err != nil {
			return err
		}
	}
	for _, l := range export.ScheduleLines {
		userID, err := r.person(l.UserEmail)
		if err != nil {
			return err
		}
		projectID, err := mapped(r.projects, l.ProjectID, "project")
		if err != nil {
			return err
		}
		if err := r.exec("schedule line", `INSERT INTO schedule_lines (team_id, user_id, project_id, created_at) VALUES (?, ?, ?, ?)`,
			r.teamID, userID, projectID, l.CreatedAt); err != nil {
			return err
		}
	}
	for _, a := range export.TimesheetApprovals {
		approver, err := r.recorder(a.ApprovedByEmail)
		if err != nil {
			return err
		}
		if err := r.exec("approved week", `INSERT INTO timesheet_approvals (team_id, starts_at, ends_at, approved_by, approved_at)
			VALUES (?, ?, ?, ?, ?)`, r.teamID, a.StartsAt, a.EndsAt, approver, a.ApprovedAt); err != nil {
			return err
		}
	}
	for _, rep := range export.SavedReports {
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
		if err := r.exec("saved report "+rep.Name, `INSERT INTO saved_reports (team_id, name, period, project_slug, tag, created_by,
			created_at, project_id, tag_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.teamID, rep.Name, rep.Period, rep.ProjectSlug, rep.Tag, creator, rep.CreatedAt, projectID, tagID); err != nil {
			return err
		}
	}
	return nil
}
