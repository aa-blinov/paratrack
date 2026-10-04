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
	// Serialize period checks with every other payroll draft creation. The
	// workspace lock makes confirmation meaningful even under concurrent posts.
	overlaps, err := payrollOverlapsTx(ctx, tx, teamID, start, end)
	if err != nil {
		return PayrollRun{}, nil, err
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
	if len(lines) == 0 {
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

func payrollOverlapsTx(ctx context.Context, tx *Tx, teamID int64, start, end time.Time) ([]PayrollRun, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, team_id, number, period_start, period_end, status, notes,
		        COALESCE(NULLIF(currency, ''), (SELECT t.currency FROM teams t WHERE t.id = payroll_runs.team_id), 'RUB'), created_at
		 FROM payroll_runs WHERE team_id = ? AND period_start < ? AND ? < period_end
		 ORDER BY period_start, id FOR UPDATE`, teamID, FormatTime(end), FormatTime(start))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PayrollRun
	for rows.Next() {
		run, err := scanPayrollRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
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
