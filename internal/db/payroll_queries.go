package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// ListPayrollRuns returns the team's pay runs, newest first.
func (d *DB) ListPayrollRuns(ctx context.Context, teamID int64) ([]PayrollRun, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, team_id, number, period_start, period_end, status, notes, COALESCE(NULLIF(currency, ''), (SELECT t.currency FROM teams t WHERE t.id = payroll_runs.team_id), 'RUB'), created_at
		 FROM payroll_runs WHERE team_id = ? ORDER BY created_at DESC`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PayrollRun
	for rows.Next() {
		r, err := scanPayrollRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListPayrollRunDetails loads a team's runs and immutable lines in one query.
func (d *DB) ListPayrollRunDetails(ctx context.Context, teamID int64) ([]model.PayrollRunDetails, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT r.id, r.team_id, r.number, r.period_start, r.period_end, r.status, r.notes,
		        COALESCE(NULLIF(r.currency, ''), (SELECT t.currency FROM teams t WHERE t.id = r.team_id), 'RUB'), r.created_at,
		        l.id, l.run_id, l.user_id, l.label, l.seconds, l.rate_cents, l.amount_cents
		 FROM payroll_runs r
		 LEFT JOIN payroll_lines l ON l.run_id = r.id
		 WHERE r.team_id = ?
		 ORDER BY r.created_at DESC, r.id DESC, l.id`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPayrollRunDetails(rows)
}

func scanPayrollRunDetails(rows *sql.Rows) ([]model.PayrollRunDetails, error) {
	byID := make(map[int64]int)
	var out []model.PayrollRunDetails
	for rows.Next() {
		var run model.PayrollRun
		var periodStart, periodEnd, createdAt string
		var lineID, lineRunID, userID, seconds, rate, amount sql.NullInt64
		var label sql.NullString
		if err := rows.Scan(&run.ID, &run.TeamID, &run.Number, &periodStart, &periodEnd, &run.Status,
			&run.Notes, &run.Currency, &createdAt, &lineID, &lineRunID, &userID, &label,
			&seconds, &rate, &amount); err != nil {
			return nil, err
		}
		if _, ok := byID[run.ID]; !ok {
			var parseErr error
			if run.PeriodStart, parseErr = ScanTime(periodStart); parseErr != nil {
				return nil, fmt.Errorf("parse payroll start for run %d: %w", run.ID, parseErr)
			}
			if run.PeriodEnd, parseErr = ScanTime(periodEnd); parseErr != nil {
				return nil, fmt.Errorf("parse payroll end for run %d: %w", run.ID, parseErr)
			}
			if run.CreatedAt, parseErr = ScanTime(createdAt); parseErr != nil {
				return nil, fmt.Errorf("parse payroll creation time for run %d: %w", run.ID, parseErr)
			}
			byID[run.ID] = len(out)
			out = append(out, model.PayrollRunDetails{Run: run})
		}
		if lineID.Valid {
			item := byID[run.ID]
			out[item].Lines = append(out[item].Lines, model.PayrollLine{
				ID: lineID.Int64, RunID: lineRunID.Int64, UserID: userID.Int64,
				Label: label.String, Seconds: int(seconds.Int64), RateCents: int(rate.Int64), AmountCents: int(amount.Int64),
			})
		}
	}
	return out, rows.Err()
}

// GetPayrollRunDetails returns the run and lines from one statement snapshot.
func (d *DB) GetPayrollRunDetails(ctx context.Context, query appmodel.PayrollRunLookupQuery) (model.PayrollRunDetails, error) {
	if query.TeamID <= 0 || query.RunID <= 0 {
		return model.PayrollRunDetails{}, ErrNotFound
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT r.id, r.team_id, r.number, r.period_start, r.period_end, r.status, r.notes,
	        COALESCE(NULLIF(r.currency, ''), (SELECT t.currency FROM teams t WHERE t.id = r.team_id), 'RUB'), r.created_at,
	        l.id, l.run_id, l.user_id, l.label, l.seconds, l.rate_cents, l.amount_cents
		 FROM payroll_runs r
		 LEFT JOIN payroll_lines l ON l.run_id = r.id
		 WHERE r.team_id = ? AND r.id = ?
		 ORDER BY l.id`, query.TeamID, query.RunID)
	if err != nil {
		return model.PayrollRunDetails{}, err
	}
	defer rows.Close()
	details, err := scanPayrollRunDetails(rows)
	if err != nil {
		return model.PayrollRunDetails{}, err
	}
	if len(details) == 0 {
		return model.PayrollRunDetails{}, ErrNotFound
	}
	return details[0], nil
}

// GetPayrollRun fetches one run.
func (d *DB) GetPayrollRun(ctx context.Context, teamID, id int64) (PayrollRun, error) {
	return getPayrollRun(ctx, d.sql, teamID, id, false)
}

func getPayrollRun(ctx context.Context, queryer queryRower, teamID, id int64, lock bool) (PayrollRun, error) {
	query := `SELECT id, team_id, number, period_start, period_end, status, notes, COALESCE(NULLIF(currency, ''), (SELECT t.currency FROM teams t WHERE t.id = payroll_runs.team_id), 'RUB'), created_at
		 FROM payroll_runs WHERE id = ? AND team_id = ?`
	if lock {
		query += ` FOR UPDATE`
	}
	row := queryer.QueryRowContext(ctx, query, id, teamID)
	r, err := scanPayrollRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return PayrollRun{}, ErrNotFound
	}
	return r, err
}

func scanPayrollRun(r interface{ Scan(...any) error }) (PayrollRun, error) {
	var (
		run            PayrollRun
		start, end, ct string
	)
	if err := r.Scan(&run.ID, &run.TeamID, &run.Number, &start, &end, &run.Status, &run.Notes, &run.Currency, &ct); err != nil {
		return PayrollRun{}, err
	}
	var err error
	if run.PeriodStart, err = ScanTime(start); err != nil {
		return PayrollRun{}, fmt.Errorf("parse payroll period start: %w", err)
	}
	if run.PeriodEnd, err = ScanTime(end); err != nil {
		return PayrollRun{}, fmt.Errorf("parse payroll period end: %w", err)
	}
	if run.CreatedAt, err = ScanTime(ct); err != nil {
		return PayrollRun{}, fmt.Errorf("parse payroll creation time: %w", err)
	}
	return run, nil
}
