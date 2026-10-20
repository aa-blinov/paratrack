package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// ExportWorkspace reads everything a manager may take out of one workspace, in
// one repeatable-read snapshot, so the file describes one moment and not a mix
// of moments. Only owners and admins may export: the file holds every member's
// rates and every invoice.
func (d *DB) ExportWorkspace(ctx context.Context, request appmodel.WorkspaceExportRequest) (appmodel.WorkspaceExport, error) {
	var out appmodel.WorkspaceExport
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return out, ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
	role, err := workspaceRole(ctx, tx, request.TeamID, request.CallerID)
	if err != nil {
		return out, err
	}
	if !role.CanManage() {
		return out, model.ErrForbidden
	}

	teamID := request.TeamID
	emails, err := userEmailsOf(ctx, tx, teamID)
	if err != nil {
		return out, err
	}
	out.Format = appmodel.WorkspaceExportFormat
	out.Version = appmodel.WorkspaceExportVersion
	out.ExportedAt = d.currentTime().UTC()
	if err := readTeamSettings(ctx, tx, teamID, &out.Team); err != nil {
		return out, err
	}
	if err := readExportMembers(ctx, tx, teamID, &out, emails); err != nil {
		return out, err
	}
	if err := readExportClientsProjects(ctx, tx, teamID, &out, emails); err != nil {
		return out, err
	}
	if err := readExportWork(ctx, tx, teamID, &out, emails); err != nil {
		return out, err
	}
	if err := readExportMoney(ctx, tx, teamID, &out, emails); err != nil {
		return out, err
	}
	if err := readExportPlans(ctx, tx, teamID, &out, emails); err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}

// Running and paused timers are left out: they are not finished work, and a
// restore must not start anyone's timer.

// workspaceRole reads the caller's role in a workspace without locking it. A
// read-only transaction cannot take row locks; writes keep using lockTeamRole.
func workspaceRole(ctx context.Context, tx *Tx, teamID, callerID int64) (model.TeamRole, error) {
	if teamID <= 0 || callerID <= 0 {
		return "", model.ErrForbidden
	}
	var role string
	if err := tx.QueryRowContext(ctx,
		`SELECT role FROM memberships WHERE team_id = ? AND user_id = ?`, teamID, callerID).Scan(&role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", model.ErrForbidden
		}
		return "", err
	}
	return model.TeamRole(role), nil
}

// userEmailsOf maps every user the workspace refers to, members or not, to the
// email the export uses for them. A user nobody in the workspace refers to is
// not in the map.
func userEmailsOf(ctx context.Context, tx *Tx, teamID int64) (map[int64]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, email FROM users WHERE id IN (
		SELECT user_id FROM memberships WHERE team_id = ?
		UNION SELECT user_id FROM former_members WHERE team_id = ?
		UNION SELECT user_id FROM sessions WHERE team_id = ?
		UNION SELECT assignee_user_id FROM tasks WHERE team_id = ?
		UNION SELECT created_by FROM tasks WHERE team_id = ?
		UNION SELECT lead_user_id FROM projects WHERE team_id = ?
		UNION SELECT user_id FROM schedule_entries WHERE team_id = ?
		UNION SELECT user_id FROM schedule_lines WHERE team_id = ?
		UNION SELECT approved_by FROM timesheet_approvals WHERE team_id = ?
		UNION SELECT created_by FROM saved_reports WHERE team_id = ?
		UNION SELECT user_id FROM payroll_lines WHERE run_id IN (SELECT id FROM payroll_runs WHERE team_id = ?)
	)`, teamID, teamID, teamID, teamID, teamID, teamID, teamID, teamID, teamID, teamID, teamID)
	if err != nil {
		return nil, fmt.Errorf("list workspace users: %w", err)
	}
	defer rows.Close()
	emails := map[int64]string{}
	for rows.Next() {
		var id int64
		var email string
		if err := rows.Scan(&id, &email); err != nil {
			return nil, err
		}
		emails[id] = email
	}
	return emails, rows.Err()
}

func readTeamSettings(ctx context.Context, tx *Tx, teamID int64, out *appmodel.ExportTeam) error {
	return tx.QueryRowContext(ctx,
		`SELECT slug, name, COALESCE(currency, ''), COALESCE(round_minutes, 0), COALESCE(round_mode, ''),
		        COALESCE(invoice_prefix, ''), COALESCE(requisites, ''), COALESCE(vat_note, ''), COALESCE(modules, ''), COALESCE(logo, '')
		 FROM teams WHERE id = ?`, teamID,
	).Scan(&out.Slug, &out.Name, &out.Currency, &out.RoundMinutes, &out.RoundMode,
		&out.InvoicePrefix, &out.Requisites, &out.VATNote, &out.Modules, &out.Logo)
}

func readExportMembers(ctx context.Context, tx *Tx, teamID int64, out *appmodel.WorkspaceExport, emails map[int64]string) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT u.email, u.name, m.role, COALESCE(m.hourly_pay_cents, 0), COALESCE(m.capacity_minutes, 0),
		        COALESCE(m.admin_minutes, 0), m.joined_at
		 FROM memberships m JOIN users u ON u.id = m.user_id WHERE m.team_id = ? ORDER BY u.email`, teamID)
	if err != nil {
		return fmt.Errorf("export members: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var m appmodel.ExportMember
		if err := rows.Scan(&m.Email, &m.Name, &m.Role, &m.HourlyPayCents, &m.CapacityMinutes, &m.AdminMinutes, &m.JoinedAt); err != nil {
			return err
		}
		out.Members = append(out.Members, m)
	}
	return rows.Err()
}

func readExportClientsProjects(ctx context.Context, tx *Tx, teamID int64, out *appmodel.WorkspaceExport, emails map[int64]string) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, name, email, details, currency, notes, archived, created_at, updated_at
		 FROM clients WHERE team_id = ? ORDER BY id`, teamID)
	if err != nil {
		return fmt.Errorf("export clients: %w", err)
	}
	for rows.Next() {
		var c appmodel.ExportClient
		var archived int64
		if err := rows.Scan(&c.ID, &c.Name, &c.Email, &c.Details, &c.Currency, &c.Notes, &archived, &c.CreatedAt, &c.UpdatedAt); err != nil {
			_ = rows.Close()
			return err
		}
		c.Archived = archived != 0
		out.Clients = append(out.Clients, c)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	rows, err = tx.QueryContext(ctx,
		`SELECT id, slug, name, color, archived, COALESCE(client_id, 0), COALESCE(client_name, ''), COALESCE(client_details, ''),
		        COALESCE(client_email, ''), billable, COALESCE(billable_rate_cents, 0), COALESCE(currency, ''),
		        COALESCE(budget_cents, 0), COALESCE(estimate_minutes, 0), COALESCE(deadline, ''), COALESCE(lead_user_id, 0),
		        created_at, updated_at
		 FROM projects WHERE team_id = ? ORDER BY id`, teamID)
	if err != nil {
		return fmt.Errorf("export projects: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p appmodel.ExportProject
		var archived, billable, lead int64
		if err := rows.Scan(&p.ID, &p.Slug, &p.Name, &p.Color, &archived, &p.ClientID, &p.ClientName, &p.ClientDetails,
			&p.ClientEmail, &billable, &p.BillableRateCents, &p.Currency, &p.BudgetCents, &p.EstimateMinutes, &p.Deadline,
			&lead, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return err
		}
		p.Archived, p.Billable, p.LeadEmail = archived != 0, billable != 0, emails[lead]
		out.Projects = append(out.Projects, p)
	}
	return rows.Err()
}

func readExportWork(ctx context.Context, tx *Tx, teamID int64, out *appmodel.WorkspaceExport, emails map[int64]string) error {
	if err := readExportActivitiesTags(ctx, tx, teamID, out); err != nil {
		return err
	}
	if err := readExportSessions(ctx, tx, teamID, out, emails); err != nil {
		return err
	}
	return readExportTasks(ctx, tx, teamID, out, emails)
}

func readExportActivitiesTags(ctx context.Context, tx *Tx, teamID int64, out *appmodel.WorkspaceExport) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, name, COALESCE(project_id, 0), archived, COALESCE(color, ''), COALESCE(task_id, 0)
		 FROM activities WHERE team_id = ? ORDER BY id`, teamID)
	if err != nil {
		return fmt.Errorf("export activities: %w", err)
	}
	for rows.Next() {
		var a appmodel.ExportActivity
		var archived int64
		if err := rows.Scan(&a.ID, &a.Name, &a.ProjectID, &archived, &a.Color, &a.TaskID); err != nil {
			_ = rows.Close()
			return err
		}
		a.Archived = archived != 0
		out.Activities = append(out.Activities, a)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	rows, err = tx.QueryContext(ctx, `SELECT id, name FROM tags WHERE team_id = ? ORDER BY id`, teamID)
	if err != nil {
		return fmt.Errorf("export tags: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tag appmodel.ExportTag
		if err := rows.Scan(&tag.ID, &tag.Name); err != nil {
			return err
		}
		out.Tags = append(out.Tags, tag)
	}
	return rows.Err()
}

func readExportSessions(ctx context.Context, tx *Tx, teamID int64, out *appmodel.WorkspaceExport, emails map[int64]string) error {
	tagsBySession := map[int64][]int64{}
	rows, err := tx.QueryContext(ctx,
		`SELECT st.session_id, st.tag_id FROM session_tags st JOIN sessions s ON s.id = st.session_id
		 WHERE s.team_id = ? ORDER BY st.session_id, st.tag_id`, teamID)
	if err != nil {
		return fmt.Errorf("export session tags: %w", err)
	}
	for rows.Next() {
		var sessionID, tagID int64
		if err := rows.Scan(&sessionID, &tagID); err != nil {
			_ = rows.Close()
			return err
		}
		tagsBySession[sessionID] = append(tagsBySession[sessionID], tagID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	rows, err = tx.QueryContext(ctx,
		`SELECT id, COALESCE(user_id, 0), activity_id, start_at, end_at, COALESCE(note, ''), paused, paused_at,
		        accumulated_seconds, last_resume_at, COALESCE(invoice_id, 0), COALESCE(external_id, ''), created_at, updated_at
		 FROM sessions WHERE team_id = ? AND end_at IS NOT NULL ORDER BY id`, teamID)
	if err != nil {
		return fmt.Errorf("export sessions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var s appmodel.ExportSession
		var user int64
		var paused int64
		var endAt, pausedAt, lastResume sql.NullString
		if err := rows.Scan(&s.ID, &user, &s.ActivityID, &s.StartAt, &endAt, &s.Note, &paused, &pausedAt,
			&s.AccumulatedSeconds, &lastResume, &s.InvoiceID, &s.ExternalID, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return err
		}
		s.UserEmail = emails[user]
		s.Paused = paused != 0
		s.EndAt, s.PausedAt, s.LastResumeAt = nullString(endAt), nullString(pausedAt), nullString(lastResume)
		s.TagIDs = tagsBySession[s.ID]
		out.Sessions = append(out.Sessions, s)
	}
	return rows.Err()
}

func readExportTasks(ctx context.Context, tx *Tx, teamID int64, out *appmodel.WorkspaceExport, emails map[int64]string) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, project_id, title, status, position, COALESCE(assignee_user_id, 0), COALESCE(due_date, ''),
		        COALESCE(estimate_minutes, 0), COALESCE(created_by, 0), archived, COALESCE(external_ref, ''),
		        COALESCE(external_url, ''), created_at, updated_at
		 FROM tasks WHERE team_id = ? ORDER BY id`, teamID)
	if err != nil {
		return fmt.Errorf("export tasks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var t appmodel.ExportTask
		var assignee, creator, archived int64
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Status, &t.Position, &assignee, &t.DueDate,
			&t.EstimateMinutes, &creator, &archived, &t.ExternalRef, &t.ExternalURL, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return err
		}
		t.AssigneeEmail, t.CreatedByEmail, t.Archived = emails[assignee], emails[creator], archived != 0
		out.Tasks = append(out.Tasks, t)
	}
	return rows.Err()
}

func readExportMoney(ctx context.Context, tx *Tx, teamID int64, out *appmodel.WorkspaceExport, emails map[int64]string) error {
	lines := map[int64][]appmodel.ExportInvoiceLine{}
	rows, err := tx.QueryContext(ctx,
		`SELECT invoice_id, label, COALESCE(detail, ''), COALESCE(seconds, 0), COALESCE(rate_cents, 0), COALESCE(amount_cents, 0)
		 FROM invoice_lines WHERE invoice_id IN (SELECT id FROM invoices WHERE team_id = ?) ORDER BY id`, teamID)
	if err != nil {
		return fmt.Errorf("export invoice lines: %w", err)
	}
	for rows.Next() {
		var invoice int64
		var line appmodel.ExportInvoiceLine
		if err := rows.Scan(&invoice, &line.Label, &line.Detail, &line.Seconds, &line.RateCents, &line.AmountCents); err != nil {
			_ = rows.Close()
			return err
		}
		lines[invoice] = append(lines[invoice], line)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	rows, err = tx.QueryContext(ctx,
		`SELECT id, number, client_name, client_email, client_details, COALESCE(project_id, 0), period_start, period_end,
		        status, notes, receipt, COALESCE(paid_at, ''), currency, seller_details, vat_note, by_person, revision, created_at
		 FROM invoices WHERE team_id = ? ORDER BY id`, teamID)
	if err != nil {
		return fmt.Errorf("export invoices: %w", err)
	}
	for rows.Next() {
		var inv appmodel.ExportInvoice
		var byPerson int64
		if err := rows.Scan(&inv.ID, &inv.Number, &inv.ClientName, &inv.ClientEmail, &inv.ClientDetails, &inv.ProjectID,
			&inv.PeriodStart, &inv.PeriodEnd, &inv.Status, &inv.Notes, &inv.Receipt, &inv.PaidAt, &inv.Currency,
			&inv.SellerDetails, &inv.VATNote, &byPerson, &inv.Revision, &inv.CreatedAt); err != nil {
			_ = rows.Close()
			return err
		}
		inv.ByPerson = byPerson != 0
		inv.Lines = lines[inv.ID]
		if inv.Lines == nil {
			inv.Lines = []appmodel.ExportInvoiceLine{}
		}
		out.Invoices = append(out.Invoices, inv)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	return readExportPayroll(ctx, tx, teamID, out, emails)
}

func readExportPayroll(ctx context.Context, tx *Tx, teamID int64, out *appmodel.WorkspaceExport, emails map[int64]string) error {
	lines := map[int64][]appmodel.ExportPayrollLine{}
	rows, err := tx.QueryContext(ctx,
		`SELECT run_id, COALESCE(user_id, 0), label, COALESCE(seconds, 0), COALESCE(rate_cents, 0), COALESCE(amount_cents, 0)
		 FROM payroll_lines WHERE run_id IN (SELECT id FROM payroll_runs WHERE team_id = ?) ORDER BY id`, teamID)
	if err != nil {
		return fmt.Errorf("export payroll lines: %w", err)
	}
	for rows.Next() {
		var run, user int64
		var line appmodel.ExportPayrollLine
		if err := rows.Scan(&run, &user, &line.Label, &line.Seconds, &line.RateCents, &line.AmountCents); err != nil {
			_ = rows.Close()
			return err
		}
		line.UserEmail = emails[user]
		lines[run] = append(lines[run], line)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	rows, err = tx.QueryContext(ctx,
		`SELECT id, number, period_start, period_end, status, notes, currency, created_at
		 FROM payroll_runs WHERE team_id = ? ORDER BY id`, teamID)
	if err != nil {
		return fmt.Errorf("export payroll runs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var run appmodel.ExportPayrollRun
		if err := rows.Scan(&run.ID, &run.Number, &run.PeriodStart, &run.PeriodEnd, &run.Status, &run.Notes, &run.Currency, &run.CreatedAt); err != nil {
			return err
		}
		run.Lines = lines[run.ID]
		if run.Lines == nil {
			run.Lines = []appmodel.ExportPayrollLine{}
		}
		out.PayrollRuns = append(out.PayrollRuns, run)
	}
	return rows.Err()
}

func readExportPlans(ctx context.Context, tx *Tx, teamID int64, out *appmodel.WorkspaceExport, emails map[int64]string) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, activity_id, period, target_minutes FROM goals WHERE team_id = ? ORDER BY id`, teamID)
	if err != nil {
		return fmt.Errorf("export goals: %w", err)
	}
	for rows.Next() {
		var g appmodel.ExportGoal
		if err := rows.Scan(&g.ID, &g.ActivityID, &g.Period, &g.TargetMinutes); err != nil {
			_ = rows.Close()
			return err
		}
		out.Goals = append(out.Goals, g)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	rows, err = tx.QueryContext(ctx,
		`SELECT user_id, project_id, day, minutes, COALESCE(note, ''), created_at FROM schedule_entries WHERE team_id = ? ORDER BY id`, teamID)
	if err != nil {
		return fmt.Errorf("export schedule entries: %w", err)
	}
	for rows.Next() {
		var user int64
		var e appmodel.ExportSchedule
		if err := rows.Scan(&user, &e.ProjectID, &e.Day, &e.Minutes, &e.Note, &e.CreatedAt); err != nil {
			_ = rows.Close()
			return err
		}
		e.UserEmail = emails[user]
		out.ScheduleEntries = append(out.ScheduleEntries, e)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	rows, err = tx.QueryContext(ctx,
		`SELECT user_id, project_id, created_at FROM schedule_lines WHERE team_id = ? ORDER BY user_id, project_id`, teamID)
	if err != nil {
		return fmt.Errorf("export schedule lines: %w", err)
	}
	for rows.Next() {
		var user int64
		var l appmodel.ExportScheduleRow
		if err := rows.Scan(&user, &l.ProjectID, &l.CreatedAt); err != nil {
			_ = rows.Close()
			return err
		}
		l.UserEmail = emails[user]
		out.ScheduleLines = append(out.ScheduleLines, l)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	rows, err = tx.QueryContext(ctx,
		`SELECT starts_at, ends_at, COALESCE(approved_by, 0), approved_at FROM timesheet_approvals WHERE team_id = ? ORDER BY id`, teamID)
	if err != nil {
		return fmt.Errorf("export approvals: %w", err)
	}
	for rows.Next() {
		var by int64
		var a appmodel.ExportApproval
		if err := rows.Scan(&a.StartsAt, &a.EndsAt, &by, &a.ApprovedAt); err != nil {
			_ = rows.Close()
			return err
		}
		a.ApprovedByEmail = emails[by]
		out.TimesheetApprovals = append(out.TimesheetApprovals, a)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	rows, err = tx.QueryContext(ctx,
		`SELECT name, period, COALESCE(project_id, 0), COALESCE(tag_id, 0), COALESCE(project_slug, ''), COALESCE(tag, ''),
		        COALESCE(created_by, 0), created_at
		 FROM saved_reports WHERE team_id = ? ORDER BY id`, teamID)
	if err != nil {
		return fmt.Errorf("export saved reports: %w", err)
	}
	for rows.Next() {
		var by int64
		var rep appmodel.ExportReport
		if err := rows.Scan(&rep.Name, &rep.Period, &rep.ProjectID, &rep.TagID, &rep.ProjectSlug, &rep.Tag, &by, &rep.CreatedAt); err != nil {
			_ = rows.Close()
			return err
		}
		rep.CreatedByEmail = emails[by]
		out.SavedReports = append(out.SavedReports, rep)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	rows, err = tx.QueryContext(ctx, `SELECT url, events, active FROM webhooks WHERE team_id = ? ORDER BY id`, teamID)
	if err != nil {
		return fmt.Errorf("export webhooks: %w", err)
	}
	for rows.Next() {
		var w appmodel.ExportWebhook
		var active int64
		if err := rows.Scan(&w.URL, &w.Events, &active); err != nil {
			_ = rows.Close()
			return err
		}
		w.Active = active != 0
		out.Webhooks = append(out.Webhooks, w)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	rows, err = tx.QueryContext(ctx,
		`SELECT provider, name, COALESCE(config, '') FROM integrations WHERE team_id = ? ORDER BY id`, teamID)
	if err != nil {
		return fmt.Errorf("export integrations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var i appmodel.ExportIntegration
		if err := rows.Scan(&i.Provider, &i.Name, &i.Config); err != nil {
			return err
		}
		out.Integrations = append(out.Integrations, i)
	}
	return rows.Err()
}

func nullString(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	v := s.String
	return &v
}
