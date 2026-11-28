package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// MarkPayrollPaidWithRecipients commits the paid transition and reads the
// notification recipients from the same locked run snapshot.
func (d *DB) MarkPayrollPaidWithRecipients(ctx context.Context, request appmodel.PayrollMutationRequest) (bool, []int64, error) {
	teamID, id, callerID := request.TeamID, request.RunID, request.CallerID
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return false, nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return false, nil, err
	}
	var status string
	if err := tx.QueryRowContext(ctx,
		`SELECT status FROM payroll_runs WHERE id = ? AND team_id = ? FOR UPDATE`, id, teamID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil, ErrNotFound
		}
		return false, nil, err
	}
	changed := status != "paid"
	if changed {
		if _, err := tx.ExecContext(ctx, `UPDATE payroll_runs SET status = 'paid' WHERE id = ? AND team_id = ?`, id, teamID); err != nil {
			return false, nil, err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT user_id FROM payroll_lines WHERE run_id = ? ORDER BY user_id`, id)
	if err != nil {
		return false, nil, err
	}
	defer rows.Close()
	var recipients []int64
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			rows.Close()
			return false, nil, err
		}
		recipients = append(recipients, userID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, nil, err
	}
	if err := rows.Close(); err != nil {
		return false, nil, err
	}
	if err := tx.Commit(); err != nil {
		return false, nil, err
	}
	return changed, recipients, nil
}

// DeletePayrollDraft removes only an unpaid run and its lines. Status is part
// of the predicate so a concurrent payment cannot race the deletion.
func (d *DB) DeletePayrollDraft(ctx context.Context, request appmodel.PayrollMutationRequest) error {
	teamID, id, callerID := request.TeamID, request.RunID, request.CallerID
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx,
		`DELETE FROM payroll_runs WHERE id = ? AND team_id = ? AND status = 'draft'`, id, teamID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var exists bool
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM payroll_runs WHERE id = ? AND team_id = ?)`, id, teamID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		return model.ErrForbidden
	}
	return tx.Commit()
}

// AddPayrollAdjustment appends a one-off bonus or deduction line to a draft run.
func (d *DB) AddPayrollAdjustment(ctx context.Context, request appmodel.PayrollAdjustmentRequest) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return err
	}
	var status string
	if err := tx.QueryRowContext(ctx,
		`SELECT status FROM payroll_runs WHERE id = ? AND team_id = ? FOR UPDATE`, request.RunID, request.TeamID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if status != "draft" {
		return model.ErrForbidden
	}
	var userName string
	if err := tx.QueryRowContext(ctx,
		`SELECT u.name FROM memberships m JOIN users u ON u.id = m.user_id WHERE m.team_id = ? AND m.user_id = ?`,
		request.TeamID, request.UserID).Scan(&userName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	label := strings.TrimSpace(userName) + " — " + strings.TrimSpace(request.Label)
	amountCents := request.AmountCents
	if request.PercentBps != 0 {
		var baseCents int
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(amount_cents), 0) FROM payroll_lines
			  WHERE run_id = ? AND user_id = ? AND kind <> ?`,
			request.RunID, request.UserID, model.PayrollLineAdjustment).Scan(&baseCents); err != nil {
			return err
		}
		if baseCents <= 0 {
			return model.ErrNoPayableTime
		}
		amountCents = percentOfCents(baseCents, request.PercentBps)
		if amountCents == 0 {
			return model.ErrNoPayableTime
		}
		pct := float64(request.PercentBps) / 100.0
		if request.PercentBps%100 == 0 {
			label = fmt.Sprintf("%s (%+d%%)", label, request.PercentBps/100)
		} else {
			label = fmt.Sprintf("%s (%+.2f%%)", label, pct)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO payroll_lines (run_id, user_id, label, seconds, rate_cents, amount_cents, kind, percent_bps) VALUES (?, ?, ?, 0, 0, ?, ?, ?)`,
		request.RunID, request.UserID, label, amountCents, model.PayrollLineAdjustment, request.PercentBps); err != nil {
		return err
	}
	return tx.Commit()
}

// DeletePayrollAdjustment removes a one-off adjustment line from a draft run.
func (d *DB) DeletePayrollAdjustment(ctx context.Context, request appmodel.PayrollAdjustmentDeleteRequest) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return err
	}
	var status string
	if err := tx.QueryRowContext(ctx,
		`SELECT status FROM payroll_runs WHERE id = ? AND team_id = ? FOR UPDATE`, request.RunID, request.TeamID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if status != "draft" {
		return model.ErrForbidden
	}
	res, err := tx.ExecContext(ctx,
		`DELETE FROM payroll_lines WHERE id = ? AND run_id = ? AND kind = ?`,
		request.LineID, request.RunID, model.PayrollLineAdjustment)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// percentOfCents takes bps basis points of base, rounding half away from zero.
func percentOfCents(base, bps int) int {
	if bps < 0 {
		return (base*bps - 5000) / 10000
	}
	return (base*bps + 5000) / 10000
}

// RecalculatePayrollDraft prices a draft run again from the timesheet as it is
// now, for the people already on it. Hours, rates and salaries are taken
// afresh; a fixed bonus or deduction stays as it was, and one given as a
// percent is taken of the new earnings. The same gates as creating a run
// apply: only approved weeks and no closed month.
func (d *DB) RecalculatePayrollDraft(ctx context.Context, request appmodel.PayrollRecalculateRequest) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return err
	}
	if _, err := lockPayrollWorkspace(ctx, tx, request.TeamID); err != nil {
		return err
	}
	run, err := getPayrollRun(ctx, tx, request.TeamID, request.RunID, true)
	if err != nil {
		return err
	}
	if run.Status != "draft" {
		return model.ErrForbidden
	}
	if err := requireApprovedPayrollTime(ctx, tx, request.TeamID, run.PeriodStart, run.PeriodEnd); err != nil {
		return err
	}
	if err := lockClosedPeriodSpan(ctx, tx, request.TeamID, run.PeriodStart, run.PeriodEnd); err != nil {
		return err
	}
	members := map[int64]bool{}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT user_id FROM payroll_lines WHERE run_id = ?`, run.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		members[id] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := lockPayrollMembers(ctx, tx, request.TeamID); err != nil {
		return err
	}
	lines, err := buildPayrollLines(ctx, tx, request.TeamID, run.PeriodStart, run.PeriodEnd, true, request.At)
	if err != nil {
		return fmt.Errorf("build payroll lines: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM payroll_lines WHERE run_id = ? AND kind <> ?`, run.ID, model.PayrollLineAdjustment); err != nil {
		return err
	}
	base := map[int64]int{}
	for _, l := range lines {
		if !members[l.UserID] {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO payroll_lines (run_id, user_id, label, seconds, rate_cents, amount_cents) VALUES (?, ?, ?, ?, ?, ?)`,
			run.ID, l.UserID, l.Label, l.Seconds, l.RateCents, l.AmountCents); err != nil {
			return err
		}
		base[l.UserID] += l.AmountCents
	}
	// The percent is applied in Go, like when it was added, so both round the
	// same way.
	for userID := range members {
		adj, err := tx.QueryContext(ctx,
			`SELECT id, percent_bps FROM payroll_lines WHERE run_id = ? AND user_id = ? AND kind = ? AND percent_bps <> 0`,
			run.ID, userID, model.PayrollLineAdjustment)
		if err != nil {
			return err
		}
		updates := map[int64]int{}
		for adj.Next() {
			var id int64
			var bps int
			if err := adj.Scan(&id, &bps); err != nil {
				adj.Close()
				return err
			}
			updates[id] = percentOfCents(base[userID], bps)
		}
		if err := adj.Err(); err != nil {
			adj.Close()
			return err
		}
		if err := adj.Close(); err != nil {
			return err
		}
		for id, amount := range updates {
			if _, err := tx.ExecContext(ctx, `UPDATE payroll_lines SET amount_cents = ? WHERE id = ?`, amount, id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
