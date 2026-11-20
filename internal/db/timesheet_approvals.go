package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// Approved weeks. A manager closes a finished week of the timesheet: from then
// on no one in the workspace can change time that starts inside it, and a pay
// run counts only time inside approved weeks. The check runs in the same
// transaction as the write, the same way the invoice lock does.

// lockApprovedWeek refuses a write to time that starts at the given instant
// when an approved week covers it for that time's owner: a whole-workspace
// approval (user_id 0) or the owner's own approval.
func lockApprovedWeek(ctx context.Context, tx *Tx, teamID int64, at time.Time, userID int64) error {
	var startsAt, endsAt string
	err := tx.QueryRowContext(ctx,
		`SELECT starts_at, ends_at FROM timesheet_approvals
		  WHERE team_id = ? AND starts_at <= ? AND ends_at > ?
		    AND (user_id = 0 OR user_id = ?)
		  ORDER BY starts_at LIMIT 1`,
		teamID, FormatTime(at), FormatTime(at), userID).Scan(&startsAt, &endsAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	from, err := ParseTime(startsAt)
	if err != nil {
		return err
	}
	to, err := ParseTime(endsAt)
	if err != nil {
		return err
	}
	return &model.SessionApprovalLockError{From: from, To: to}
}

// lockApprovedSpan refuses a write to any span that overlaps an approved week.
// Checking only the two ends is not enough: an approved window strictly inside
// the span (a single approved day inside a row clear) would pass unseen.
// A whole-workspace approval (user_id 0) blocks the span outright; an approval
// for one member blocks it only when that member has sessions in the overlap
// of the span and the approved window — the time the write would disturb.
func lockApprovedSpan(ctx context.Context, tx *Tx, teamID int64, from, to time.Time) error {
	var startsAt, endsAt string
	err := tx.QueryRowContext(ctx,
		`SELECT a.starts_at, a.ends_at FROM timesheet_approvals a
		  WHERE a.team_id = ? AND a.starts_at < ? AND a.ends_at > ?
		    AND (a.user_id = 0 OR EXISTS (
		      SELECT 1 FROM sessions s
		       WHERE s.team_id = a.team_id AND s.user_id = a.user_id
		         AND s.start_at >= a.starts_at AND s.start_at < a.ends_at
		         AND s.start_at >= ? AND s.start_at < ?
		    ))
		  ORDER BY a.starts_at LIMIT 1`,
		teamID, FormatTime(to), FormatTime(from), FormatTime(from), FormatTime(to)).Scan(&startsAt, &endsAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	approvedFrom, err := ParseTime(startsAt)
	if err != nil {
		return err
	}
	approvedTo, err := ParseTime(endsAt)
	if err != nil {
		return err
	}
	return &model.SessionApprovalLockError{From: approvedFrom, To: approvedTo}
}

// ApproveTimesheetWeek closes one week. Only a manager may do it, and only for a
// week that has ended and holds no running timer: a timer still running would
// change the week's totals after the approval. A positive UserID closes the
// week for that member alone; 0 closes it for the whole workspace.
func (d *DB) ApproveTimesheetWeek(ctx context.Context, request appmodel.TimesheetApprovalRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 || !request.End.After(request.Start) || request.UserID < 0 {
		return ErrNotFound
	}
	now := d.currentTime().UTC()
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, role, err := lockTeamRole(ctx, tx, request.TeamID, request.CallerID)
	if err != nil {
		return err
	}
	if !role.CanManage() {
		return model.ErrForbidden
	}
	if request.UserID > 0 {
		// The person must be a member: approving a stranger's week would
		// create a row that never matches any time.
		if err := checkMemberInTeam(ctx, tx, request.UserID, request.TeamID); err != nil {
			return err
		}
	}
	if request.End.After(now) {
		return model.ErrWeekNotFinished
	}
	var running int64
	// A member-scoped approval only guards that member's timers; the team-wide
	// one guards everyone's, as it always has.
	runningQuery := `SELECT COUNT(*) FROM sessions WHERE team_id = ? AND end_at IS NULL AND start_at < ?`
	runningArgs := []any{request.TeamID, FormatTime(request.End)}
	if request.UserID > 0 {
		runningQuery += ` AND user_id = ?`
		runningArgs = append(runningArgs, request.UserID)
	}
	if err := tx.QueryRowContext(ctx, runningQuery, runningArgs...).Scan(&running); err != nil {
		return err
	}
	if running > 0 {
		return model.ErrWeekHasRunningTimer
	}
	// Approving the same week twice is not an error: the first approval stands.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO timesheet_approvals (team_id, starts_at, ends_at, approved_by, approved_at, user_id)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (team_id, starts_at, user_id) DO NOTHING`,
		request.TeamID, FormatTime(request.Start), FormatTime(request.End),
		request.CallerID, FormatTime(now), request.UserID); err != nil {
		return err
	}
	return tx.Commit()
}

// ReopenTimesheetWeek removes an approval so the week can be edited again. A
// week that a pay run already covers stays closed: changing it would change a
// number someone was paid from. Reopening the workspace-wide row does not
// remove member rows of the same week, and vice versa: each approval is its
// own decision.
func (d *DB) ReopenTimesheetWeek(ctx context.Context, request appmodel.TimesheetApprovalRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 || !request.End.After(request.Start) || request.UserID < 0 {
		return ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, role, err := lockTeamRole(ctx, tx, request.TeamID, request.CallerID)
	if err != nil {
		return err
	}
	if !role.CanManage() {
		return model.ErrForbidden
	}
	var covered int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM payroll_runs
		  WHERE team_id = ? AND period_start < ? AND period_end > ?`,
		request.TeamID, FormatTime(request.End), FormatTime(request.Start)).Scan(&covered); err != nil {
		return err
	}
	if covered > 0 {
		return model.ErrWeekPaid
	}
	res, err := tx.ExecContext(ctx,
		`DELETE FROM timesheet_approvals WHERE team_id = ? AND starts_at = ? AND ends_at = ? AND user_id = ?`,
		request.TeamID, FormatTime(request.Start), FormatTime(request.End), request.UserID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// ListTimesheetApprovals returns the approved weeks that overlap [from, to).
// A member row and the workspace-wide row of the same week are two entries.
func (d *DB) ListTimesheetApprovals(ctx context.Context, teamID int64, from, to time.Time) ([]appmodel.TimesheetApproval, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT starts_at, ends_at, approved_by, approved_at, user_id FROM timesheet_approvals
		  WHERE team_id = ? AND starts_at < ? AND ends_at > ?
		  ORDER BY starts_at, user_id`,
		teamID, FormatTime(to), FormatTime(from))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]appmodel.TimesheetApproval, 0)
	for rows.Next() {
		var startsAt, endsAt, approvedAt string
		var by int64
		item := appmodel.TimesheetApproval{ApprovedBy: by}
		if err := rows.Scan(&startsAt, &endsAt, &by, &approvedAt, &item.UserID); err != nil {
			return nil, err
		}
		if item.Start, err = ParseTime(startsAt); err != nil {
			return nil, err
		}
		if item.End, err = ParseTime(endsAt); err != nil {
			return nil, err
		}
		if item.ApprovedAt, err = ParseTime(approvedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// requireApprovedPayrollTime refuses a pay run that would count closed time
// outside every approved week. The error says how much time is missing and
// over which dates, so the manager knows which weeks to approve.
func requireApprovedPayrollTime(ctx context.Context, tx *Tx, teamID int64, from, to time.Time) error {
	notApproved, err := unapprovedPayrollTime(ctx, tx, teamID, from, to)
	if err != nil {
		return err
	}
	if notApproved != nil {
		return notApproved
	}
	return nil
}

// UnapprovedPayrollTime reports the closed time in [from, to) that no approved
// week covers, or nil when all of it is approved. The payroll page uses it to
// warn before a run is refused.
func (d *DB) UnapprovedPayrollTime(ctx context.Context, teamID int64, from, to time.Time) (*model.PayrollNotApprovedError, error) {
	return unapprovedPayrollTime(ctx, d.sql, teamID, from, to)
}

// UnapprovedPayrollStarts lists the start of every closed or running session in
// [from, to) that no approved week covers, oldest first. It is the same rule as
// UnapprovedPayrollTime, for a whole window in one query: the payroll page groups
// the starts by week instead of asking once per week.
func (d *DB) UnapprovedPayrollStarts(ctx context.Context, teamID int64, from, to time.Time) ([]time.Time, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT s.start_at FROM sessions s
		  WHERE s.team_id = ?
		    AND s.start_at >= ? AND s.start_at < ?
		    AND NOT EXISTS (
		      SELECT 1 FROM timesheet_approvals a
		       WHERE a.team_id = s.team_id AND a.starts_at <= s.start_at AND a.ends_at > s.start_at
		         AND (a.user_id = 0 OR a.user_id = s.user_id))
		  ORDER BY s.start_at`,
		teamID, FormatTime(from), FormatTime(to))
	if err != nil {
		return nil, fmt.Errorf("list unapproved payroll starts: %w", err)
	}
	defer rows.Close()
	var starts []time.Time
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		at, err := ParseTime(raw)
		if err != nil {
			return nil, err
		}
		starts = append(starts, at)
	}
	return starts, rows.Err()
}

// unapprovedPayrollTime reports the closed time in [from, to) that no approved
// week covers, or nil when all of it is approved. The payroll page uses it to
// warn before a run is refused.
func unapprovedPayrollTime(ctx context.Context, q queryRower, teamID int64, from, to time.Time) (*model.PayrollNotApprovedError, error) {
	var count int64
	var first, last sql.NullString
	err := q.QueryRowContext(ctx,
		// Running timers count too: a pay run prices them at now, so they must sit
		// inside an approved week like closed time does. A week approved for one
		// member covers that member's time and nobody else's (TIME-23).
		`SELECT COUNT(*), MIN(s.start_at), MAX(s.start_at) FROM sessions s
		  WHERE s.team_id = ?
		    AND s.start_at >= ? AND s.start_at < ?
		    AND NOT EXISTS (
		      SELECT 1 FROM timesheet_approvals a
		       WHERE a.team_id = s.team_id AND a.starts_at <= s.start_at AND a.ends_at > s.start_at
		         AND (a.user_id = 0 OR a.user_id = s.user_id))`,
		teamID, FormatTime(from), FormatTime(to)).Scan(&count, &first, &last)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	firstAt, err := ParseTime(first.String)
	if err != nil {
		return nil, err
	}
	lastAt, err := ParseTime(last.String)
	if err != nil {
		return nil, err
	}
	return &model.PayrollNotApprovedError{Count: int(count), First: firstAt, Last: lastAt}, nil
}

// lockApprovedSessionStart checks a session by its stored start time and owner.
func lockApprovedSessionStart(ctx context.Context, tx *Tx, teamID int64, startAt string, userID int64) error {
	at, err := ParseTime(startAt)
	if err != nil {
		return err
	}
	return lockApprovedWeek(ctx, tx, teamID, at, userID)
}

// lockApprovedSessionByID checks a session that a write is about to change, by
// its id, inside the same transaction.
func lockApprovedSessionByID(ctx context.Context, tx *Tx, teamID, sessionID int64) error {
	var startAt string
	var userID int64
	err := tx.QueryRowContext(ctx,
		`SELECT start_at, user_id FROM sessions WHERE id = ? AND team_id = ?`, sessionID, teamID).Scan(&startAt, &userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return lockApprovedSessionStart(ctx, tx, teamID, startAt, userID)
}

// ListUnapprovedTimeWeeks returns the first days (YYYY-MM-DD) of the weeks
// that hold billable time not yet on an invoice and outside every approved
// week, in order. The predicate is the one unbilled time uses, so the
// invoice warning covers exactly the time that would land on the invoice
// (FIN-12). The weeks are cut in the workspace's own zone and from its own
// first weekday, the same way the timesheet shows them, so the answer names
// a week the manager can actually approve. A value that is not a timestamp
// stays out: a corrupted row must not break the warning query.
func (d *DB) ListUnapprovedTimeWeeks(ctx context.Context, teamID, projectID int64, from, to time.Time) ([]string, error) {
	if teamID <= 0 {
		return nil, ErrInvalidInvoiceQuery
	}
	inner := `SELECT ((s.start_at::timestamptz) AT TIME ZONE COALESCE(NULLIF(t.timezone, ''), 'UTC'))::date AS day,
			       (CASE WHEN t.week_start = 'sunday'
			             THEN (EXTRACT(ISODOW FROM (s.start_at::timestamptz) AT TIME ZONE COALESCE(NULLIF(t.timezone, ''), 'UTC')))::int % 7
			             ELSE (EXTRACT(ISODOW FROM (s.start_at::timestamptz) AT TIME ZONE COALESCE(NULLIF(t.timezone, ''), 'UTC')))::int - 1
			        END) AS back
			FROM sessions s
			JOIN teams t ON t.id = s.team_id
			JOIN activities a ON a.id = s.activity_id AND a.team_id = ?
			JOIN projects p ON p.id = COALESCE(s.project_id, a.project_id) AND p.team_id = ?
			WHERE s.team_id = ? AND s.end_at IS NOT NULL AND s.invoice_id IS NULL
			  AND s.start_at >= ? AND s.start_at < ?
			  AND s.start_at ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}'
			  AND COALESCE(p.billable, 1) = 1 AND COALESCE(p.billable_rate_cents, 0) > 0 AND p.archived = 0`
	args := []any{teamID, teamID, teamID, FormatTime(from), FormatTime(to)}
	if projectID > 0 {
		inner += ` AND p.id = ?`
		args = append(args, projectID)
	}
	inner += `
			  AND NOT EXISTS (
			    SELECT 1 FROM timesheet_approvals ap
			     WHERE ap.team_id = s.team_id AND ap.starts_at <= s.start_at AND ap.ends_at > s.start_at
			       AND (ap.user_id = 0 OR ap.user_id = s.user_id))`
	q := `SELECT DISTINCT to_char(w.day - w.back, 'YYYY-MM-DD')
		FROM (` + inner + `
		) w
		ORDER BY 1`
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var weeks []string
	for rows.Next() {
		var week string
		if err := rows.Scan(&week); err != nil {
			return nil, err
		}
		weeks = append(weeks, week)
	}
	return weeks, rows.Err()
}
