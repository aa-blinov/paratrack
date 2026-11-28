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

// Closing a month as a whole (FIN-25). A closed month fixes the hours,
// invoices and payouts inside it: session writes, invoice drafts and pay runs
// overlap-check the table next to the approved-week and autolock guards, and
// a refused write carries the month back to the person. Reopening is the
// manager's escape hatch, the same role that approves weeks holds.
//
// The month lives in this table as the workspace's own calendar date (the
// first day, "2006-01-02"), never as a UTC instant: an instant formed at
// midnight in one zone reads as the last day of the month before in another,
// and the close would fix the wrong month.

// CloseMonth marks one month closed. The month must be over: a closed running
// month would strand the timers that are still open inside it. Closing twice
// is not an error: the first close stands, like approval's.
func (d *DB) CloseMonth(ctx context.Context, request appmodel.PeriodCloseRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 {
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
	if !role.CanReadFinance() {
		return model.ErrForbidden
	}
	key, err := periodKey(request.Month)
	if err != nil {
		return err
	}
	loc, err := workspaceZone(ctx, tx, request.TeamID)
	if err != nil {
		return err
	}
	// Finished means the workspace's own calendar has moved past the month.
	now := d.currentTime().In(loc)
	if now.Year()*12+int(now.Month()) <= key.year*12+key.month {
		return model.ErrMonthNotFinished
	}
	// A timer still running from inside the month could not be stopped once the
	// month is closed, so the close names it instead of swallowing it.
	var running int64
	monthStart := time.Date(key.year, time.Month(key.month), 1, 0, 0, 0, 0, loc)
	monthEnd := monthStart.AddDate(0, 1, 0)
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions WHERE team_id = ? AND end_at IS NULL AND start_at < ?`,
		request.TeamID, FormatTime(monthEnd)).Scan(&running); err != nil {
		return err
	}
	if running > 0 {
		return model.ErrWeekHasRunningTimer
	}
	// A draft left in the month could still be issued, edited or deleted
	// around the close, and an invoice whose letter is on its way flips to
	// sent after it; the month closes once its documents are settled.
	var drafts int
	if err := tx.QueryRowContext(ctx,
		`SELECT (SELECT COUNT(*) FROM invoices WHERE team_id = ? AND status IN ('draft', 'sending') AND period_start < ? AND period_end > ?)
		      + (SELECT COUNT(*) FROM payroll_runs WHERE team_id = ? AND status = 'draft' AND period_start < ? AND period_end > ?)`,
		request.TeamID, FormatTime(monthEnd), FormatTime(monthStart), request.TeamID, FormatTime(monthEnd), FormatTime(monthStart)).Scan(&drafts); err != nil {
		return err
	}
	if drafts > 0 {
		return model.ErrMonthHasDrafts
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO closed_periods (team_id, month, closed_at, closed_by, closed_by_role)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (team_id, month) DO NOTHING`,
		request.TeamID, key.day, FormatTime(d.currentTime().UTC()), request.CallerID, string(role)); err != nil {
		return fmt.Errorf("close month for team %d: %w", request.TeamID, err)
	}
	return tx.Commit()
}

// ReopenMonth removes the mark so the month can change again. The pay-run and
// invoice locks keep their own protection: reopening the bookkeeping gate does
// not re-open documents someone was already paid from.
func (d *DB) ReopenMonth(ctx context.Context, request appmodel.PeriodCloseRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 {
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
	if !role.CanReadFinance() {
		return model.ErrForbidden
	}
	key, err := periodKey(request.Month)
	if err != nil {
		return err
	}
	// The owner reopens any month and whoever closed a month may reopen it.
	// An admin reopens the rest, except a month an accountant closed: the
	// books handed in are not reopened behind the accountant's back.
	if !role.IsOwner() {
		var closedBy int64
		var closerRole sql.NullString
		err := tx.QueryRowContext(ctx,
			`SELECT c.closed_by, COALESCE(NULLIF(c.closed_by_role, ''), m.role) FROM closed_periods c
			   LEFT JOIN memberships m ON m.team_id = c.team_id AND m.user_id = c.closed_by
			  WHERE c.team_id = ? AND c.month = ?`, request.TeamID, key.day).Scan(&closedBy, &closerRole)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		mine := closedBy == request.CallerID
		byAccountant := closerRole.String == string(model.TeamRoleAccountant)
		if !mine && (role != model.TeamRoleAdmin || byAccountant) {
			return model.ErrForbidden
		}
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM closed_periods WHERE team_id = ? AND month = ?`,
		request.TeamID, key.day); err != nil {
		return fmt.Errorf("reopen month for team %d: %w", request.TeamID, err)
	}
	return tx.Commit()
}

// ListClosedMonths reads the closed months as the payouts page shows them to
// the accountant, newest first. The month reads back in the workspace zone.
func (d *DB) ListClosedMonths(ctx context.Context, teamID int64) ([]appmodel.PeriodClose, error) {
	loc, err := workspaceZone(ctx, d.sql, teamID)
	if err != nil {
		return nil, err
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT c.month, c.closed_at, c.closed_by, COALESCE(NULLIF(c.closed_by_role, ''), m.role, '') FROM closed_periods c
		   LEFT JOIN memberships m ON m.team_id = c.team_id AND m.user_id = c.closed_by
		  WHERE c.team_id = ? ORDER BY c.month DESC`,
		teamID)
	if err != nil {
		return nil, fmt.Errorf("list closed months for team %d: %w", teamID, err)
	}
	defer rows.Close()
	var out []appmodel.PeriodClose
	for rows.Next() {
		var item appmodel.PeriodClose
		var month, closedAt string
		if err := rows.Scan(&month, &closedAt, &item.ClosedByID, &item.ClosedByRole); err != nil {
			return nil, err
		}
		if item.Month, err = time.ParseInLocation("2006-01-02", month, loc); err != nil {
			return nil, fmt.Errorf("parse closed month %s: %w", month, err)
		}
		if item.ClosedOn, err = ParseTime(closedAt); err != nil {
			return nil, fmt.Errorf("parse closed month time %s: %w", closedAt, err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// lockClosedMonth refuses a write to time that starts inside a closed month.
// It runs in the same transaction as the write, next to the approved-week and
// autolock checks.
func lockClosedMonth(ctx context.Context, tx *Tx, teamID int64, at time.Time) error {
	loc, err := workspaceZone(ctx, tx, teamID)
	if err != nil {
		return err
	}
	monthStart := monthStartIn(at, loc)
	var one int
	err = tx.QueryRowContext(ctx,
		`SELECT 1 FROM closed_periods WHERE team_id = ? AND month = ?`, teamID, monthStart.Format("2006-01-02")).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return &model.PeriodClosedError{Month: monthStart}
}

// lockClosedPeriodSpan refuses a document whose period overlaps a closed
// month, naming the earliest one. A span that is not after its start reads as
// the empty span.
func lockClosedPeriodSpan(ctx context.Context, tx *Tx, teamID int64, from, to time.Time) error {
	if !to.After(from) {
		return nil
	}
	loc, err := workspaceZone(ctx, tx, teamID)
	if err != nil {
		return err
	}
	first := monthStartIn(from, loc).Format("2006-01-02")
	last := monthStartIn(to.Add(-time.Nanosecond), loc).Format("2006-01-02")
	var month string
	err = tx.QueryRowContext(ctx,
		`SELECT month FROM closed_periods WHERE team_id = ? AND month >= ? AND month <= ? ORDER BY month LIMIT 1`,
		teamID, first, last).Scan(&month)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	closed, err := time.ParseInLocation("2006-01-02", month, loc)
	if err != nil {
		return fmt.Errorf("parse closed month %s: %w", month, err)
	}
	return &model.PeriodClosedError{Month: closed}
}

// periodKey normalises the request's month name. The day it takes must be the
// month's first day in the workspace zone; guards write and read the exact
// same string.
type periodKeyValue struct {
	day   string
	year  int
	month int
}

func periodKey(month string) (periodKeyValue, error) {
	parsed, err := time.Parse("2006-01-02", month)
	if err != nil {
		return periodKeyValue{}, fmt.Errorf("read the month: %w", err)
	}
	// Any day inside the month names the whole month: normalise to its first
	// day, so guards and the stored row write one identical key.
	first := time.Date(parsed.Year(), parsed.Month(), 1, 0, 0, 0, 0, time.UTC)
	return periodKeyValue{day: first.Format("2006-01-02"), year: first.Year(), month: int(first.Month())}, nil
}

// workspaceZone is the workspace's calendar zone; a blank one means UTC, the
// fallback the rest of the workspace calendar uses.
func workspaceZone(ctx context.Context, q queryRower, teamID int64) (*time.Location, error) {
	var zone string
	if err := q.QueryRowContext(ctx,
		`SELECT timezone FROM teams WHERE id = ?`, teamID).Scan(&zone); err != nil {
		return nil, err
	}
	if zone == "" {
		return time.UTC, nil
	}
	parsed, err := time.LoadLocation(zone)
	if err != nil {
		return nil, fmt.Errorf("load workspace time zone: %w", err)
	}
	return parsed, nil
}

// lockClosedMonthSessionByID checks a session that a write is about to
// change, by its id. A session that is gone is left to the write itself to
// report.
func lockClosedMonthSessionByID(ctx context.Context, tx *Tx, teamID, sessionID int64) error {
	var startAt string
	err := tx.QueryRowContext(ctx,
		`SELECT start_at FROM sessions WHERE id = ? AND team_id = ?`, sessionID, teamID).Scan(&startAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	at, err := ParseTime(startAt)
	if err != nil {
		return err
	}
	return lockClosedMonth(ctx, tx, teamID, at)
}

// monthStartIn is the first day of at's month in loc. time.Date normalises a
// day before the first of the month into the month before.
func monthStartIn(at time.Time, loc *time.Location) time.Time {
	local := at.In(loc)
	return time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
}

// lockInvoiceOpenPeriodTx refuses a change to an invoice whose period
// overlaps a closed month: a draft left in a month the books handed over
// cannot be edited, rebuilt or issued there either.
func lockInvoiceOpenPeriodTx(ctx context.Context, tx *Tx, teamID, invoiceID int64) error {
	var start, end string
	if err := tx.QueryRowContext(ctx,
		`SELECT period_start, period_end FROM invoices WHERE id = ? AND team_id = ?`, invoiceID, teamID).Scan(&start, &end); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	from, err := ScanTime(start)
	if err != nil {
		return err
	}
	to, err := ScanTime(end)
	if err != nil {
		return err
	}
	return lockClosedPeriodSpan(ctx, tx, teamID, from, to)
}

// lockPayrollOpenPeriodTx is lockInvoiceOpenPeriodTx for a pay run.
func lockPayrollOpenPeriodTx(ctx context.Context, tx *Tx, teamID, runID int64) error {
	var start, end string
	if err := tx.QueryRowContext(ctx,
		`SELECT period_start, period_end FROM payroll_runs WHERE id = ? AND team_id = ?`, runID, teamID).Scan(&start, &end); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	from, err := ScanTime(start)
	if err != nil {
		return err
	}
	to, err := ScanTime(end)
	if err != nil {
		return err
	}
	return lockClosedPeriodSpan(ctx, tx, teamID, from, to)
}
