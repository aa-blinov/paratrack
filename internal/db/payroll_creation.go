package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

type preparedPayrollRunRequest struct {
	TeamID   int64
	CallerID int64
	Number   string
	Notes    string
	Start    time.Time
	End      time.Time
	Lines    []model.PayrollLine
}

// createPayrollRun stores a prepared payroll snapshot for persistence tests.
// Production creation uses CreatePayrollDraft so calculation and writes share
// one locked transaction.
func (d *DB) createPayrollRun(ctx context.Context, request preparedPayrollRunRequest) (PayrollRun, error) {
	teamID, callerID := request.TeamID, request.CallerID
	number, notes, start, end, lines := request.Number, request.Notes, request.Start, request.End, request.Lines
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return PayrollRun{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return PayrollRun{}, err
	}
	currency, err := lockPayrollWorkspace(ctx, tx, teamID)
	if err != nil {
		return PayrollRun{}, err
	}
	at := d.currentTime().UTC()
	runID, err := insertPayrollRunTx(ctx, tx, teamID, number, notes, start, end, currency, lines, at)
	if err != nil {
		return PayrollRun{}, err
	}
	run, err := getPayrollRun(ctx, tx, teamID, runID, true)
	if err != nil {
		return PayrollRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return PayrollRun{}, err
	}
	return run, nil
}

// CreatePayrollDraft uses one caller-supplied instant for live-session
// pricing, the run number's year, and the run creation timestamp.
func (d *DB) CreatePayrollDraft(ctx context.Context, request appmodel.PayrollDraftRequest) (PayrollRun, []PayrollRun, error) {
	teamID, callerID, notes := request.TeamID, request.CallerID, request.Notes
	at, start, end, allowOverlap := request.CreatedAt, request.Start, request.End, request.ConfirmOverlap
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return PayrollRun{}, nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return PayrollRun{}, nil, err
	}
	currency, err := lockPayrollWorkspace(ctx, tx, teamID)
	if err != nil {
		return PayrollRun{}, nil, err
	}
	// A pay run counts only time in approved weeks: unapproved time would be
	// paid before anyone has checked it.
	if err := requireApprovedPayrollTime(ctx, tx, teamID, start, end); err != nil {
		return PayrollRun{}, nil, err
	}
	// A closed month pays nothing more: the payout reads numbers that were
	// fixed when the bookkeeping gate closed (FIN-25).
	if err := lockClosedPeriodSpan(ctx, tx, teamID, start, end); err != nil {
		return PayrollRun{}, nil, err
	}
	// Serialize period checks with every other payroll draft creation. The
	// workspace lock makes confirmation meaningful even under concurrent posts.
	userFilter := makeUserFilter(request.UserIDs)
	overlaps, err := payrollOverlapsTx(ctx, tx, teamID, start, end, userFilter)
	if err != nil {
		return PayrollRun{}, nil, err
	}
	// Two runs can price one month without their periods meeting: a salary
	// pays the calendar month, not the hours. Runs that already paid a
	// salary to a member who is still salaried count as overlaps when they
	// touch any month the new run covers, so paying a month twice is a
	// confirmed choice with a written reason, the same as re-paying hours
	// (FIN-22).
	salaried, err := salaryOverlapsTx(ctx, tx, teamID, start, end, userFilter)
	if err != nil {
		return PayrollRun{}, nil, err
	}
	seen := make(map[int64]bool, len(overlaps))
	for _, run := range overlaps {
		seen[run.ID] = true
	}
	for _, run := range salaried {
		if !seen[run.ID] {
			overlaps = append(overlaps, run)
			seen[run.ID] = true
		}
	}
	if len(overlaps) > 0 && !allowOverlap {
		return PayrollRun{}, overlaps, nil
	}
	if err := lockPayrollMembers(ctx, tx, teamID); err != nil {
		return PayrollRun{}, nil, err
	}
	lines, err := buildPayrollLines(ctx, tx, teamID, start, end, true, at)
	if err != nil {
		return PayrollRun{}, nil, fmt.Errorf("build payroll lines: %w", err)
	}
	if len(userFilter) > 0 {
		filtered := make([]PayrollLine, 0, len(lines))
		for _, l := range lines {
			if userFilter[l.UserID] {
				filtered = append(filtered, l)
			}
		}
		lines = filtered
	}
	if len(lines) == 0 {
		// Say which of the two it is: nobody has a pay, or nobody tracked
		// time. A salary counts as a pay (FIN-26) even though it prices no
		// tracked time.
		var rated int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM memberships WHERE team_id = ?
			  AND (COALESCE(hourly_pay_cents, 0) > 0 OR (COALESCE(pay_kind, 'hourly') = 'monthly' AND COALESCE(monthly_pay_cents, 0) > 0))`, teamID).Scan(&rated); err != nil {
			return PayrollRun{}, nil, err
		}
		if rated == 0 {
			return PayrollRun{}, nil, model.ErrNoPayRates
		}
		return PayrollRun{}, nil, model.ErrNoPayableTime
	}
	runID, err := insertPayrollRunTx(ctx, tx, teamID, "", notes, start, end, currency, lines, at)
	if err != nil {
		return PayrollRun{}, nil, err
	}
	run, err := getPayrollRun(ctx, tx, teamID, runID, true)
	if err != nil {
		return PayrollRun{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return PayrollRun{}, nil, err
	}
	return run, nil, nil
}

func makeUserFilter(ids []int64) map[int64]bool {
	if len(ids) == 0 {
		return nil
	}
	out := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id > 0 {
			out[id] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func runTouchesFilteredUsers(ctx context.Context, tx *Tx, runID int64, userFilter map[int64]bool) (bool, error) {
	if len(userFilter) == 0 {
		return true, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT user_id FROM payroll_lines WHERE run_id = ?`, runID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			return false, err
		}
		if userFilter[uid] {
			return true, nil
		}
	}
	return false, rows.Err()
}

func payrollOverlapsTx(ctx context.Context, tx *Tx, teamID int64, start, end time.Time, userFilter map[int64]bool) ([]PayrollRun, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, team_id, number, period_start, period_end, status, notes,
		        COALESCE(NULLIF(currency, ''), (SELECT t.currency FROM teams t WHERE t.id = payroll_runs.team_id), 'RUB'), created_at
		 FROM payroll_runs WHERE team_id = ? AND period_start < ? AND ? < period_end
		 ORDER BY period_start, id FOR UPDATE`, teamID, FormatTime(end), FormatTime(start))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var candidates []PayrollRun
	for rows.Next() {
		run, err := scanPayrollRun(rows)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, run)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(userFilter) == 0 {
		return candidates, nil
	}
	var out []PayrollRun
	for _, run := range candidates {
		ok, err := runTouchesFilteredUsers(ctx, tx, run.ID, userFilter)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, run)
		}
	}
	return out, nil
}

// salaryOverlapsTx lists the runs that already paid a salary line to a member
// who is still salaried (or left with a salary) and whose period touches any
// calendar month the new run covers. A salary line is the one with no hours
// and no rate that is not a hand-added adjustment: hourly lines always carry
// a positive rate, and a bonus carries kind = 'adjustment'.
func salaryOverlapsTx(ctx context.Context, tx *Tx, teamID int64, start, end time.Time, userFilter map[int64]bool) ([]PayrollRun, error) {
	first := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, start.Location())
	last := end.Add(-time.Nanosecond)
	monthEnd := time.Date(last.Year(), last.Month(), 1, 0, 0, 0, 0, last.Location()).AddDate(0, 1, 0)
	rows, err := tx.QueryContext(ctx,
		`SELECT id, team_id, number, period_start, period_end, status, notes,
		        COALESCE(NULLIF(currency, ''), (SELECT t.currency FROM teams t WHERE t.id = payroll_runs.team_id), 'RUB'), created_at
		 FROM payroll_runs
		 WHERE team_id = ? AND period_start < ? AND ? < period_end
		   AND id IN (
		       SELECT DISTINCT l.run_id FROM payroll_lines l
		        WHERE l.seconds = 0 AND l.rate_cents = 0 AND l.amount_cents > 0 AND l.kind <> 'adjustment'
		          AND EXISTS (
		              SELECT 1 FROM memberships m
		               WHERE m.team_id = payroll_runs.team_id AND m.user_id = l.user_id
		                 AND COALESCE(m.pay_kind, 'hourly') = 'monthly' AND COALESCE(m.monthly_pay_cents, 0) > 0
		              UNION ALL
		              SELECT 1 FROM former_members f
		               WHERE f.team_id = payroll_runs.team_id AND f.user_id = l.user_id
		                 AND COALESCE(f.pay_kind, 'hourly') = 'monthly' AND COALESCE(f.monthly_pay_cents, 0) > 0
		          )
		   )
		 ORDER BY period_start, id FOR UPDATE`,
		teamID, FormatTime(monthEnd), FormatTime(first))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var candidates []PayrollRun
	for rows.Next() {
		run, err := scanPayrollRun(rows)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, run)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(userFilter) == 0 {
		return candidates, nil
	}
	var out []PayrollRun
	for _, run := range candidates {
		ok, err := runTouchesFilteredUsers(ctx, tx, run.ID, userFilter)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, run)
		}
	}
	return out, nil
}

func lockPayrollWorkspace(ctx context.Context, tx *Tx, teamID int64) (string, error) {
	var currency string
	if err := tx.QueryRowContext(ctx, `SELECT currency FROM teams WHERE id = ? FOR UPDATE`, teamID).Scan(&currency); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("lock payroll workspace: %w", err)
	}
	return currency, nil
}

func lockPayrollMembers(ctx context.Context, tx *Tx, teamID int64) error {
	queries := []string{
		`SELECT user_id FROM memberships WHERE team_id = ? ORDER BY user_id FOR UPDATE`,
		`SELECT f.user_id FROM former_members f
		 WHERE f.team_id = ? AND NOT EXISTS (
		   SELECT 1 FROM memberships m WHERE m.team_id = f.team_id AND m.user_id = f.user_id)
		 ORDER BY f.user_id FOR UPDATE OF f`,
	}
	for _, query := range queries {
		rows, err := tx.QueryContext(ctx, query, teamID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var userID int64
			if err := rows.Scan(&userID); err != nil {
				rows.Close()
				return err
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	return nil
}

func insertPayrollRunTx(ctx context.Context, tx *Tx, teamID int64, number, notes string,
	start, end time.Time, currency string, lines []PayrollLine, at time.Time) (int64, error) {
	if number == "" {
		year := at.Year()
		prefix := fmt.Sprintf("PAY-%d-", year)
		var maxNum int
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(CAST(SUBSTR(number, LENGTH(?) + 1) AS INTEGER)), 0)
			 FROM payroll_runs WHERE team_id = ? AND number LIKE ?`,
			prefix, teamID, prefix+"%").Scan(&maxNum); err != nil {
			return 0, fmt.Errorf("allocate payroll number: %w", err)
		}
		number = fmt.Sprintf("%s%03d", prefix, maxNum+1)
	}
	now := FormatTime(at.UTC())
	var runID int64
	err := tx.QueryRowContext(ctx,
		`INSERT INTO payroll_runs (team_id, number, period_start, period_end, status, notes, currency, created_at)
		 VALUES (?, ?, ?, ?, 'draft', ?, ?, ?) RETURNING id`,
		teamID, number, FormatTime(start), FormatTime(end), notes, currency, now).Scan(&runID)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrDuplicate
		}
		return 0, err
	}
	for _, l := range lines {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO payroll_lines (run_id, user_id, label, seconds, rate_cents, amount_cents)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			runID, l.UserID, l.Label, l.Seconds, l.RateCents, l.AmountCents); err != nil {
			return 0, err
		}
	}
	return runID, nil
}
