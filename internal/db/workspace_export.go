package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// ExportWorkspace hands everything a manager may take out of one workspace to the
// request's sink, record by record, in one repeatable-read snapshot, so the file describes
// one moment and not a mix of moments. Records are read as they are handed on, so
// a large workspace never sits in memory whole. Only owners and admins may
// export: the file holds every member's rates and every invoice.
func (d *DB) ExportWorkspace(ctx context.Context, request appmodel.WorkspaceExportRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.Sink == nil {
		return ErrNotFound
	}
	sink := request.Sink
	tx, err := d.sql.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	role, err := workspaceRole(ctx, tx, request.TeamID, request.CallerID)
	if err != nil {
		return err
	}
	if !role.CanManage() {
		return model.ErrForbidden
	}

	teamID := request.TeamID
	emails, err := userEmailsOf(ctx, tx, teamID)
	if err != nil {
		return err
	}
	if err := sink.Header(d.currentTime().UTC()); err != nil {
		return err
	}
	team, err := readTeamSettings(ctx, tx, teamID)
	if err != nil {
		return err
	}
	if err := sink.Team(team); err != nil {
		return err
	}
	// The order is the order a restore needs: a list only refers to lists before it.
	steps := []func() error{
		func() error { return exportMembers(ctx, tx, teamID, sink) },
		func() error { return exportClients(ctx, tx, teamID, sink) },
		func() error { return exportProjects(ctx, tx, teamID, emails, sink) },
		func() error { return exportTags(ctx, tx, teamID, sink) },
		func() error { return exportTasks(ctx, tx, teamID, emails, sink) },
		func() error { return exportActivities(ctx, tx, teamID, sink) },
		func() error { return exportGoals(ctx, tx, teamID, sink) },
		func() error { return exportInvoices(ctx, tx, teamID, sink) },
		func() error { return exportPayrollRuns(ctx, tx, teamID, emails, sink) },
		func() error { return exportSessions(ctx, tx, teamID, emails, sink) },
		func() error { return exportSchedule(ctx, tx, teamID, emails, sink) },
		func() error { return exportApprovals(ctx, tx, teamID, emails, sink) },
		func() error { return exportReports(ctx, tx, teamID, emails, sink) },
		func() error { return exportWebhooks(ctx, tx, teamID, sink) },
		func() error { return exportIntegrations(ctx, tx, teamID, sink) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return tx.Commit()
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

// rowQueryer is what a query runs on: a transaction or the database itself.
type rowQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// queryRows runs query and calls each for every row, which Scans the row it is
// given. The rows are closed when the loop ends, whatever it returns. what names
// the query in an error.
func queryRows(ctx context.Context, q rowQueryer, what string, query string, each func(rows *sql.Rows) error, args ...any) error {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	defer rows.Close()
	for rows.Next() {
		if err := each(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// userEmailsOf maps every user the workspace refers to, members or not, to the
// email the export uses for them. A user nobody in the workspace refers to is
// not in the map.
func userEmailsOf(ctx context.Context, tx *Tx, teamID int64) (map[int64]string, error) {
	emails := map[int64]string{}
	err := queryRows(ctx, tx, "list workspace users", `SELECT id, email FROM users WHERE id IN (
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
	)`, func(rows *sql.Rows) error {
		var id int64
		var email string
		if err := rows.Scan(&id, &email); err != nil {
			return err
		}
		emails[id] = email
		return nil
	}, teamID, teamID, teamID, teamID, teamID, teamID, teamID, teamID, teamID, teamID, teamID)
	return emails, err
}

func readTeamSettings(ctx context.Context, tx *Tx, teamID int64) (appmodel.ExportTeam, error) {
	var out appmodel.ExportTeam
	err := tx.QueryRowContext(ctx,
		`SELECT slug, name, COALESCE(currency, ''), COALESCE(round_minutes, 0), COALESCE(round_mode, ''),
		        COALESCE(invoice_prefix, ''), COALESCE(requisites, ''), COALESCE(vat_note, ''), COALESCE(modules, ''), COALESCE(logo, '')
		 FROM teams WHERE id = ?`, teamID,
	).Scan(&out.Slug, &out.Name, &out.Currency, &out.RoundMinutes, &out.RoundMode,
		&out.InvoicePrefix, &out.Requisites, &out.VATNote, &out.Modules, &out.Logo)
	return out, err
}

func exportMembers(ctx context.Context, tx *Tx, teamID int64, sink appmodel.WorkspaceSink) error {
	return queryRows(ctx, tx, "export members",
		`SELECT u.email, u.name, m.role, COALESCE(m.hourly_pay_cents, 0), COALESCE(m.capacity_minutes, 0),
		        COALESCE(m.admin_minutes, 0), m.joined_at
		 FROM memberships m JOIN users u ON u.id = m.user_id WHERE m.team_id = ? ORDER BY u.email`,
		func(rows *sql.Rows) error {
			var m appmodel.ExportMember
			if err := rows.Scan(&m.Email, &m.Name, &m.Role, &m.HourlyPayCents, &m.CapacityMinutes, &m.AdminMinutes, &m.JoinedAt); err != nil {
				return err
			}
			return sink.Member(m)
		}, teamID)
}

func exportClients(ctx context.Context, tx *Tx, teamID int64, sink appmodel.WorkspaceSink) error {
	return queryRows(ctx, tx, "export clients",
		`SELECT id, name, email, details, currency, notes, archived, created_at, updated_at
		 FROM clients WHERE team_id = ? ORDER BY id`,
		func(rows *sql.Rows) error {
			var c appmodel.ExportClient
			var archived int64
			if err := rows.Scan(&c.ID, &c.Name, &c.Email, &c.Details, &c.Currency, &c.Notes, &archived, &c.CreatedAt, &c.UpdatedAt); err != nil {
				return err
			}
			c.Archived = archived != 0
			return sink.Client(c)
		}, teamID)
}

func exportProjects(ctx context.Context, tx *Tx, teamID int64, emails map[int64]string, sink appmodel.WorkspaceSink) error {
	return queryRows(ctx, tx, "export projects",
		`SELECT id, slug, name, color, archived, COALESCE(client_id, 0), COALESCE(client_name, ''), COALESCE(client_details, ''),
		        COALESCE(client_email, ''), billable, COALESCE(billable_rate_cents, 0), COALESCE(currency, ''),
		        COALESCE(budget_cents, 0), COALESCE(estimate_minutes, 0), COALESCE(deadline, ''), COALESCE(lead_user_id, 0),
		        created_at, updated_at
		 FROM projects WHERE team_id = ? ORDER BY id`,
		func(rows *sql.Rows) error {
			var p appmodel.ExportProject
			var archived, billable, lead int64
			if err := rows.Scan(&p.ID, &p.Slug, &p.Name, &p.Color, &archived, &p.ClientID, &p.ClientName, &p.ClientDetails,
				&p.ClientEmail, &billable, &p.BillableRateCents, &p.Currency, &p.BudgetCents, &p.EstimateMinutes, &p.Deadline,
				&lead, &p.CreatedAt, &p.UpdatedAt); err != nil {
				return err
			}
			p.Archived, p.Billable, p.LeadEmail = archived != 0, billable != 0, emails[lead]
			return sink.Project(p)
		}, teamID)
}

func exportTags(ctx context.Context, tx *Tx, teamID int64, sink appmodel.WorkspaceSink) error {
	return queryRows(ctx, tx, "export tags", `SELECT id, name FROM tags WHERE team_id = ? ORDER BY id`,
		func(rows *sql.Rows) error {
			var tag appmodel.ExportTag
			if err := rows.Scan(&tag.ID, &tag.Name); err != nil {
				return err
			}
			return sink.Tag(tag)
		}, teamID)
}

func exportTasks(ctx context.Context, tx *Tx, teamID int64, emails map[int64]string, sink appmodel.WorkspaceSink) error {
	return queryRows(ctx, tx, "export tasks",
		`SELECT id, project_id, title, status, position, COALESCE(assignee_user_id, 0), COALESCE(due_date, ''),
		        COALESCE(estimate_minutes, 0), COALESCE(created_by, 0), archived, COALESCE(external_ref, ''),
		        COALESCE(external_url, ''), created_at, updated_at, COALESCE(description, '')
		 FROM tasks WHERE team_id = ? ORDER BY id`,
		func(rows *sql.Rows) error {
			var t appmodel.ExportTask
			var assignee, creator, archived int64
			if err := rows.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Status, &t.Position, &assignee, &t.DueDate,
				&t.EstimateMinutes, &creator, &archived, &t.ExternalRef, &t.ExternalURL, &t.CreatedAt, &t.UpdatedAt, &t.Description); err != nil {
				return err
			}
			t.AssigneeEmail, t.CreatedByEmail, t.Archived = emails[assignee], emails[creator], archived != 0
			return sink.Task(t)
		}, teamID)
}

func exportActivities(ctx context.Context, tx *Tx, teamID int64, sink appmodel.WorkspaceSink) error {
	return queryRows(ctx, tx, "export activities",
		`SELECT id, name, COALESCE(project_id, 0), archived, COALESCE(color, ''), COALESCE(task_id, 0)
		 FROM activities WHERE team_id = ? ORDER BY id`,
		func(rows *sql.Rows) error {
			var a appmodel.ExportActivity
			var archived int64
			if err := rows.Scan(&a.ID, &a.Name, &a.ProjectID, &archived, &a.Color, &a.TaskID); err != nil {
				return err
			}
			a.Archived = archived != 0
			return sink.Activity(a)
		}, teamID)
}

func exportGoals(ctx context.Context, tx *Tx, teamID int64, sink appmodel.WorkspaceSink) error {
	return queryRows(ctx, tx, "export goals",
		`SELECT id, activity_id, period, target_minutes FROM goals WHERE team_id = ? ORDER BY id`,
		func(rows *sql.Rows) error {
			var g appmodel.ExportGoal
			if err := rows.Scan(&g.ID, &g.ActivityID, &g.Period, &g.TargetMinutes); err != nil {
				return err
			}
			return sink.Goal(g)
		}, teamID)
}

// exportInvoices reads the invoice lines first, then each invoice with its lines.
// Session ids and payment-provider links are not part of an invoice.
func exportInvoices(ctx context.Context, tx *Tx, teamID int64, sink appmodel.WorkspaceSink) error {
	payments := map[int64][]appmodel.ExportInvoicePayment{}
	if err := queryRows(ctx, tx, "export invoice payments",
		`SELECT invoice_id, paid_on, amount_cents, note, source, created_at FROM invoice_payments WHERE team_id = ? ORDER BY id`,
		func(rows *sql.Rows) error {
			var invoice int64
			var payment appmodel.ExportInvoicePayment
			if err := rows.Scan(&invoice, &payment.PaidOn, &payment.AmountCents, &payment.Note, &payment.Source, &payment.CreatedAt); err != nil {
				return err
			}
			payments[invoice] = append(payments[invoice], payment)
			return nil
		}, teamID); err != nil {
		return err
	}
	lines := map[int64][]appmodel.ExportInvoiceLine{}
	err := queryRows(ctx, tx, "export invoice lines",
		`SELECT invoice_id, label, COALESCE(detail, ''), COALESCE(seconds, 0), COALESCE(rate_cents, 0), COALESCE(amount_cents, 0)
		 FROM invoice_lines WHERE invoice_id IN (SELECT id FROM invoices WHERE team_id = ?) ORDER BY id`,
		func(rows *sql.Rows) error {
			var invoice int64
			var line appmodel.ExportInvoiceLine
			if err := rows.Scan(&invoice, &line.Label, &line.Detail, &line.Seconds, &line.RateCents, &line.AmountCents); err != nil {
				return err
			}
			lines[invoice] = append(lines[invoice], line)
			return nil
		}, teamID)
	if err != nil {
		return err
	}

	return queryRows(ctx, tx, "export invoices",
		`SELECT id, number, client_name, client_email, client_details, COALESCE(project_id, 0), period_start, period_end,
		        status, notes, receipt, COALESCE(paid_at, ''), COALESCE(due_date, ''), currency, seller_details, vat_note, by_person, revision, created_at
		 FROM invoices WHERE team_id = ? ORDER BY id`,
		func(rows *sql.Rows) error {
			var inv appmodel.ExportInvoice
			var byPerson int64
			if err := rows.Scan(&inv.ID, &inv.Number, &inv.ClientName, &inv.ClientEmail, &inv.ClientDetails, &inv.ProjectID,
				&inv.PeriodStart, &inv.PeriodEnd, &inv.Status, &inv.Notes, &inv.Receipt, &inv.PaidAt, &inv.DueDate, &inv.Currency,
				&inv.SellerDetails, &inv.VATNote, &byPerson, &inv.Revision, &inv.CreatedAt); err != nil {
				return err
			}
			inv.ByPerson = byPerson != 0
			inv.Lines = lines[inv.ID]
			if inv.Lines == nil {
				inv.Lines = []appmodel.ExportInvoiceLine{}
			}
			inv.Payments = payments[inv.ID]
			if inv.Payments == nil {
				inv.Payments = []appmodel.ExportInvoicePayment{}
			}
			return sink.Invoice(inv)
		}, teamID)
}

func exportPayrollRuns(ctx context.Context, tx *Tx, teamID int64, emails map[int64]string, sink appmodel.WorkspaceSink) error {
	lines := map[int64][]appmodel.ExportPayrollLine{}
	err := queryRows(ctx, tx, "export payroll lines",
		`SELECT run_id, COALESCE(user_id, 0), label, COALESCE(seconds, 0), COALESCE(rate_cents, 0), COALESCE(amount_cents, 0)
		 FROM payroll_lines WHERE run_id IN (SELECT id FROM payroll_runs WHERE team_id = ?) ORDER BY id`,
		func(rows *sql.Rows) error {
			var run, user int64
			var line appmodel.ExportPayrollLine
			if err := rows.Scan(&run, &user, &line.Label, &line.Seconds, &line.RateCents, &line.AmountCents); err != nil {
				return err
			}
			line.UserEmail = emails[user]
			lines[run] = append(lines[run], line)
			return nil
		}, teamID)
	if err != nil {
		return err
	}

	return queryRows(ctx, tx, "export payroll runs",
		`SELECT id, number, period_start, period_end, status, notes, currency, created_at
		 FROM payroll_runs WHERE team_id = ? ORDER BY id`,
		func(rows *sql.Rows) error {
			var run appmodel.ExportPayrollRun
			if err := rows.Scan(&run.ID, &run.Number, &run.PeriodStart, &run.PeriodEnd, &run.Status, &run.Notes, &run.Currency, &run.CreatedAt); err != nil {
				return err
			}
			run.Lines = lines[run.ID]
			if run.Lines == nil {
				run.Lines = []appmodel.ExportPayrollLine{}
			}
			return sink.PayrollRun(run)
		}, teamID)
}

// exportSessions reads each finished session with its tags in one row. The tags
// come as one packed column, so the sessions stream without a second query running
// on the same transaction.
func exportSessions(ctx context.Context, tx *Tx, teamID int64, emails map[int64]string, sink appmodel.WorkspaceSink) error {
	return queryRows(ctx, tx, "export sessions",
		`SELECT s.id, COALESCE(s.user_id, 0), s.activity_id, s.start_at, s.end_at, COALESCE(s.note, ''), s.paused, s.paused_at,
		        s.accumulated_seconds, s.last_resume_at, COALESCE(s.invoice_id, 0), COALESCE(s.external_id, ''), s.created_at, s.updated_at,
		        COALESCE((SELECT string_agg(st.tag_id::text, ',' ORDER BY st.tag_id) FROM session_tags st WHERE st.session_id = s.id), '')
		 FROM sessions s WHERE s.team_id = ? AND s.end_at IS NOT NULL ORDER BY s.id`,
		func(rows *sql.Rows) error {
			var s appmodel.ExportSession
			var user, paused int64
			var endAt, pausedAt, lastResume sql.NullString
			var tags string
			if err := rows.Scan(&s.ID, &user, &s.ActivityID, &s.StartAt, &endAt, &s.Note, &paused, &pausedAt,
				&s.AccumulatedSeconds, &lastResume, &s.InvoiceID, &s.ExternalID, &s.CreatedAt, &s.UpdatedAt, &tags); err != nil {
				return err
			}
			ids, err := parseTagIDs(tags)
			if err != nil {
				return err
			}
			s.TagIDs = ids
			s.UserEmail = emails[user]
			s.Paused = paused != 0
			s.EndAt, s.PausedAt, s.LastResumeAt = nullString(endAt), nullString(pausedAt), nullString(lastResume)
			return sink.Session(s)
		}, teamID)
}

// parseTagIDs reads the comma-separated tag ids a session row carries. A session
// with no tags has an empty list, never a null.
func parseTagIDs(packed string) ([]int64, error) {
	ids := []int64{}
	if packed == "" {
		return ids, nil
	}
	for _, part := range strings.Split(packed, ",") {
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("export session tags: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func exportSchedule(ctx context.Context, tx *Tx, teamID int64, emails map[int64]string, sink appmodel.WorkspaceSink) error {
	err := queryRows(ctx, tx, "export schedule entries",
		`SELECT user_id, project_id, day, minutes, COALESCE(note, ''), created_at FROM schedule_entries WHERE team_id = ? ORDER BY id`,
		func(rows *sql.Rows) error {
			var user int64
			var e appmodel.ExportSchedule
			if err := rows.Scan(&user, &e.ProjectID, &e.Day, &e.Minutes, &e.Note, &e.CreatedAt); err != nil {
				return err
			}
			e.UserEmail = emails[user]
			return sink.Schedule(e)
		}, teamID)
	if err != nil {
		return err
	}

	return queryRows(ctx, tx, "export schedule lines",
		`SELECT user_id, project_id, created_at FROM schedule_lines WHERE team_id = ? ORDER BY user_id, project_id`,
		func(rows *sql.Rows) error {
			var user int64
			var l appmodel.ExportScheduleRow
			if err := rows.Scan(&user, &l.ProjectID, &l.CreatedAt); err != nil {
				return err
			}
			l.UserEmail = emails[user]
			return sink.ScheduleRow(l)
		}, teamID)
}

func exportApprovals(ctx context.Context, tx *Tx, teamID int64, emails map[int64]string, sink appmodel.WorkspaceSink) error {
	return queryRows(ctx, tx, "export approvals",
		`SELECT starts_at, ends_at, COALESCE(approved_by, 0), approved_at FROM timesheet_approvals WHERE team_id = ? ORDER BY id`,
		func(rows *sql.Rows) error {
			var by int64
			var a appmodel.ExportApproval
			if err := rows.Scan(&a.StartsAt, &a.EndsAt, &by, &a.ApprovedAt); err != nil {
				return err
			}
			a.ApprovedByEmail = emails[by]
			return sink.Approval(a)
		}, teamID)
}

func exportReports(ctx context.Context, tx *Tx, teamID int64, emails map[int64]string, sink appmodel.WorkspaceSink) error {
	return queryRows(ctx, tx, "export saved reports",
		`SELECT name, period, COALESCE(project_id, 0), COALESCE(tag_id, 0), COALESCE(project_slug, ''), COALESCE(tag, ''),
		        COALESCE(created_by, 0), created_at
		 FROM saved_reports WHERE team_id = ? ORDER BY id`,
		func(rows *sql.Rows) error {
			var by int64
			var rep appmodel.ExportReport
			if err := rows.Scan(&rep.Name, &rep.Period, &rep.ProjectID, &rep.TagID, &rep.ProjectSlug, &rep.Tag, &by, &rep.CreatedAt); err != nil {
				return err
			}
			rep.CreatedByEmail = emails[by]
			return sink.Report(rep)
		}, teamID)
}

// exportWebhooks keeps the endpoint and its events. The signing secret stays here.
func exportWebhooks(ctx context.Context, tx *Tx, teamID int64, sink appmodel.WorkspaceSink) error {
	return queryRows(ctx, tx, "export webhooks", `SELECT url, events, active FROM webhooks WHERE team_id = ? ORDER BY id`,
		func(rows *sql.Rows) error {
			var w appmodel.ExportWebhook
			var active int64
			if err := rows.Scan(&w.URL, &w.Events, &active); err != nil {
				return err
			}
			w.Active = active != 0
			return sink.Webhook(w)
		}, teamID)
}

func exportIntegrations(ctx context.Context, tx *Tx, teamID int64, sink appmodel.WorkspaceSink) error {
	return queryRows(ctx, tx, "export integrations",
		`SELECT provider, name, COALESCE(config, '') FROM integrations WHERE team_id = ? ORDER BY id`,
		func(rows *sql.Rows) error {
			var i appmodel.ExportIntegration
			if err := rows.Scan(&i.Provider, &i.Name, &i.Config); err != nil {
				return err
			}
			return sink.Integration(i)
		}, teamID)
}

func nullString(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	v := s.String
	return &v
}
