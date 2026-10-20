package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// richWorkspace is a workspace with every kind of record the export holds, built
// through the same paths the product uses where it has them.
type richWorkspace struct {
	teamID     int64
	ownerID    int64
	memberID   int64
	clientID   int64
	projectID  int64
	taskID     int64
	activityID int64
}

// seedRichWorkspace builds one workspace with an owner, a member, a client, a
// billable project, a task with its activity, finished time from both people
// with a tag, a billed invoice, a payroll run, the schedule, an approved week and
// a saved report. A running timer is left on it, which the export must not hold.
func seedRichWorkspace(t *testing.T, d *DB, slug string) richWorkspace {
	t.Helper()
	ctx := t.Context()
	ownerID := seedApprovalUser(t, d, slug+"-owner@test.local")
	memberID := seedApprovalUser(t, d, slug+"-member@test.local")
	teamID, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: slug, Name: "Студия " + slug, OwnerID: ownerID})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	now := FormatTime(time.Now().UTC())
	sql := d.TestSQL()
	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := sql.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("setup %q: %v", query, err)
		}
	}
	mustExec(`INSERT INTO memberships (team_id, user_id, role, joined_at, hourly_pay_cents, capacity_minutes, admin_minutes) VALUES (?, ?, 'member', ?, 150000, 2400, 60)`, teamID, memberID, now)
	mustExec(`UPDATE users SET password_hash = 'HASH_SECRET_TEST' WHERE id = ?`, ownerID)
	mustExec(`UPDATE teams SET stripe_key = 'sk_live_SECRET_TEST', stripe_webhook_secret = 'whsec_SECRET_TEST', currency = 'RUB', invoice_prefix = 'INV', round_minutes = 15, round_mode = 'up', logo = 'data:image/png;base64,LOGO_TEST', requisites = 'ИП Иванов, ИНН 1' WHERE id = ?`, teamID)

	var clientID int64
	if err := sql.QueryRowContext(ctx, `INSERT INTO clients (team_id, name, email, details, currency, notes, archived, created_at, updated_at) VALUES (?, 'Ромашка', 'buy@romashka.test', 'ИНН 1', 'RUB', '', 0, ?, ?) RETURNING id`, teamID, now, now).Scan(&clientID); err != nil {
		t.Fatalf("client: %v", err)
	}
	proj, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Сайт", Color: "#7c3aed"})
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	mustExec(`UPDATE projects SET client_id = ?, billable = 1, billable_rate_cents = 500000, lead_user_id = ?, deadline = '2026-12-31' WHERE id = ?`, clientID, ownerID, proj.ID)
	task, err := d.CreateTask(ctx, appmodel.TaskCreateRequest{TeamID: teamID, CallerID: ownerID, ProjectID: proj.ID, Title: "Вёрстка", Status: model.TaskStatusDoing, AssigneeID: memberID, EstimateMinutes: 240})
	if err != nil {
		t.Fatalf("task: %v", err)
	}
	var activityID int64
	if err := sql.QueryRowContext(ctx, `SELECT id FROM activities WHERE task_id = ?`, task.ID).Scan(&activityID); err != nil {
		t.Fatalf("task activity: %v", err)
	}
	var reviewID int64
	if err := sql.QueryRowContext(ctx, `INSERT INTO activities (name, team_id, project_id, archived, color, created_at, updated_at, task_id) VALUES ('Ревью', ?, ?, 0, '#16a34a', ?, ?, ?) RETURNING id`, teamID, proj.ID, now, now, task.ID).Scan(&reviewID); err != nil {
		t.Fatalf("second activity: %v", err)
	}

	ownerCtx := requestctx.WithActor(ctx, ownerID)
	memberCtx := requestctx.WithScope(requestctx.WithActor(ctx, memberID), memberID)
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	ownerSession, err := d.CreateClosedSession(ownerCtx, appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activityID, Start: start, End: start.Add(time.Hour), Note: "верстка шапки"})
	if err != nil {
		t.Fatalf("owner session: %v", err)
	}
	if _, err := d.CreateClosedSession(memberCtx, appmodel.TimerAddRequest{TeamID: teamID, ActivityID: reviewID, Start: start.Add(2 * time.Hour), End: start.Add(3 * time.Hour)}); err != nil {
		t.Fatalf("member session: %v", err)
	}
	if _, err := d.StartSession(ownerCtx, appmodel.TimerStartRequest{TeamID: teamID, ActivityID: activityID, At: time.Now().UTC()}); err != nil {
		t.Fatalf("running timer: %v", err)
	}
	var tagID int64
	if err := sql.QueryRowContext(ctx, `INSERT INTO tags (name, team_id, created_at) VALUES ('срочно', ?, ?) RETURNING id`, teamID, now).Scan(&tagID); err != nil {
		t.Fatalf("tag: %v", err)
	}
	mustExec(`INSERT INTO session_tags (session_id, tag_id) VALUES (?, ?)`, ownerSession.ID, tagID)

	var invoiceID int64
	if err := sql.QueryRowContext(ctx, `INSERT INTO invoices (team_id, number, client_name, client_email, client_details, period_start, period_end, status, notes, payment_url, stripe_session_id, created_at, currency, seller_details, vat_note, project_id, receipt, by_person, revision)
		VALUES (?, 'INV-2026-001', 'Ромашка', 'buy@romashka.test', 'ИНН 1', '2026-09-01', '2026-09-30', 'sent', 'к оплате до 10-го', 'https://pay.example/SECRET_LINK', 'cs_SECRET_SESSION', ?, 'RUB', 'ИП Иванов', '', ?, 'квитанция 17', 1, 2) RETURNING id`, teamID, now, proj.ID).Scan(&invoiceID); err != nil {
		t.Fatalf("invoice: %v", err)
	}
	mustExec(`UPDATE sessions SET invoice_id = ? WHERE id = ?`, invoiceID, ownerSession.ID)
	mustExec(`INSERT INTO invoice_lines (invoice_id, label, detail, seconds, rate_cents, amount_cents) VALUES (?, 'Вёрстка', 'главная', 3600, 500000, 500000)`, invoiceID)
	var runID int64
	if err := sql.QueryRowContext(ctx, `INSERT INTO payroll_runs (team_id, number, period_start, period_end, status, notes, created_at, currency) VALUES (?, 'PAY-2026-001', '2026-09-01', '2026-09-30', 'draft', 'сентябрь', ?, 'RUB') RETURNING id`, teamID, now).Scan(&runID); err != nil {
		t.Fatalf("payroll run: %v", err)
	}
	mustExec(`INSERT INTO payroll_lines (run_id, user_id, label, seconds, rate_cents, amount_cents) VALUES (?, ?, 'Сентябрь', 3600, 150000, 150000)`, runID, memberID)
	mustExec(`INSERT INTO schedule_entries (team_id, user_id, project_id, day, minutes, note, created_at) VALUES (?, ?, ?, '2026-09-02', 240, 'каркас', ?)`, teamID, memberID, proj.ID, now)
	mustExec(`INSERT INTO schedule_lines (team_id, user_id, project_id, created_at) VALUES (?, ?, ?, ?)`, teamID, memberID, proj.ID, now)
	mustExec(`INSERT INTO timesheet_approvals (team_id, starts_at, ends_at, approved_by, approved_at) VALUES (?, '2026-08-31', '2026-09-06', ?, ?)`, teamID, ownerID, now)
	mustExec(`INSERT INTO goals (activity_id, team_id, period, target_minutes, created_at, updated_at) VALUES (?, ?, 'weekly', 600, ?, ?)`, activityID, teamID, now, now)
	mustExec(`INSERT INTO saved_reports (team_id, name, period, project_slug, tag, created_by, created_at, project_id, tag_id) VALUES (?, 'За сентябрь', 'month', 'site', '', ?, ?, ?, ?)`, teamID, ownerID, now, proj.ID, tagID)
	mustExec(`INSERT INTO webhooks (team_id, url, secret, events, active, created_at) VALUES (?, 'https://hooks.example/paratrack', 'whsec_SECRET_HOOK', 'session.stopped', 1, ?)`, teamID, now)
	mustExec(`INSERT INTO integrations (team_id, provider, name, secret, config, sync_generation, last_sync_error, created_at) VALUES (?, 'github', 'Основной', 'gh_SECRET_TOKEN', '{"target":"acme/site"}', 0, '', ?)`, teamID, now)
	return richWorkspace{teamID: teamID, ownerID: ownerID, memberID: memberID, clientID: clientID, projectID: proj.ID, taskID: task.ID, activityID: activityID}
}
