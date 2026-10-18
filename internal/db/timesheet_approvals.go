package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// Approved weeks. A manager closes a finished week of the timesheet: from then
// on no one in the workspace can change time that starts inside it, and a pay
// run counts only time inside approved weeks. The check runs in the same
// transaction as the write, the same way the invoice lock does.

// lockApprovedWeek refuses a write to time that starts at the given instant
// when an approved week covers it.
func lockApprovedWeek(ctx context.Context, tx *Tx, teamID int64, at time.Time) error {
	var startsAt, endsAt string
	err := tx.QueryRowContext(ctx,
		`SELECT starts_at, ends_at FROM timesheet_approvals
		  WHERE team_id = ? AND starts_at <= ? AND ends_at > ?
		  ORDER BY starts_at LIMIT 1`,
		teamID, FormatTime(at), FormatTime(at)).Scan(&startsAt, &endsAt)
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

// lockApprovedSpan refuses a write to a span that touches an approved week at
// either end. Weeks are whole days, so checking both ends covers the span.
func lockApprovedSpan(ctx context.Context, tx *Tx, teamID int64, from, to time.Time) error {
	if err := lockApprovedWeek(ctx, tx, teamID, from); err != nil {
		return err
	}
	return lockApprovedWeek(ctx, tx, teamID, to.Add(-time.Nanosecond))
}

// ApproveTimesheetWeek closes one week. Only a manager may do it, and only for a
// week that has ended and holds no running timer: a timer still running would
// change the week's totals after the approval.
func (d *DB) ApproveTimesheetWeek(ctx context.Context, request appmodel.TimesheetApprovalRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 || !request.End.After(request.Start) {
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
	if request.End.After(now) {
		return model.ErrWeekNotFinished
	}
	var running int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions
		  WHERE team_id = ? AND end_at IS NULL AND start_at < ?`,
		request.TeamID, FormatTime(request.End)).Scan(&running); err != nil {
		return err
	}
	if running > 0 {
		return model.ErrWeekHasRunningTimer
	}
	// Approving the same week twice is not an error: the first approval stands.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO timesheet_approvals (team_id, starts_at, ends_at, approved_by, approved_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (team_id, starts_at) DO NOTHING`,
		request.TeamID, FormatTime(request.Start), FormatTime(request.End),
		request.CallerID, FormatTime(now)); err != nil {
		return err
	}
	return tx.Commit()
}

// ReopenTimesheetWeek removes an approval so the week can be edited again. A
// week that a pay run already covers stays closed: changing it would change a
// number someone was paid from.
func (d *DB) ReopenTimesheetWeek(ctx context.Context, request appmodel.TimesheetApprovalRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 || !request.End.After(request.Start) {
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
		`DELETE FROM timesheet_approvals WHERE team_id = ? AND starts_at = ? AND ends_at = ?`,
		request.TeamID, FormatTime(request.Start), FormatTime(request.End))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// ListTimesheetApprovals returns the approved weeks that overlap [from, to).
func (d *DB) ListTimesheetApprovals(ctx context.Context, teamID int64, from, to time.Time) ([]appmodel.TimesheetApproval, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT starts_at, ends_at, approved_by, approved_at FROM timesheet_approvals
		  WHERE team_id = ? AND starts_at < ? AND ends_at > ?
		  ORDER BY starts_at`,
		teamID, FormatTime(to), FormatTime(from))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]appmodel.TimesheetApproval, 0)
	for rows.Next() {
		var startsAt, endsAt, approvedAt string
		var by int64
		if err := rows.Scan(&startsAt, &endsAt, &by, &approvedAt); err != nil {
			return nil, err
		}
		item := appmodel.TimesheetApproval{ApprovedBy: by}
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
	var count int64
	var first, last sql.NullString
	err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*), MIN(s.start_at), MAX(s.start_at) FROM sessions s
		  WHERE s.team_id = ? AND s.end_at IS NOT NULL
		    AND s.start_at >= ? AND s.start_at < ?
		    AND NOT EXISTS (
		      SELECT 1 FROM timesheet_approvals a
		       WHERE a.team_id = s.team_id AND a.starts_at <= s.start_at AND a.ends_at > s.start_at)`,
		teamID, FormatTime(from), FormatTime(to)).Scan(&count, &first, &last)
	if err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	firstAt, err := ParseTime(first.String)
	if err != nil {
		return err
	}
	lastAt, err := ParseTime(last.String)
	if err != nil {
		return err
	}
	return &model.PayrollNotApprovedError{Count: int(count), First: firstAt, Last: lastAt}
}

// lockApprovedSessionStart checks a session by its stored start time.
func lockApprovedSessionStart(ctx context.Context, tx *Tx, teamID int64, startAt string) error {
	at, err := ParseTime(startAt)
	if err != nil {
		return err
	}
	return lockApprovedWeek(ctx, tx, teamID, at)
}

// lockApprovedSessionByID checks a session that a write is about to change, by
// its id, inside the same transaction.
func lockApprovedSessionByID(ctx context.Context, tx *Tx, teamID, sessionID int64) error {
	var startAt string
	err := tx.QueryRowContext(ctx,
		`SELECT start_at FROM sessions WHERE id = ? AND team_id = ?`, sessionID, teamID).Scan(&startAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return lockApprovedSessionStart(ctx, tx, teamID, startAt)
}
