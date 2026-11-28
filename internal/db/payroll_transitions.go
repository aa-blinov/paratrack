package db

import (
	"context"
	"database/sql"
	"errors"
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
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO payroll_lines (run_id, user_id, label, seconds, rate_cents, amount_cents) VALUES (?, ?, ?, 0, 0, ?)`,
		request.RunID, request.UserID, label, request.AmountCents); err != nil {
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
		`DELETE FROM payroll_lines WHERE id = ? AND run_id = ? AND seconds = 0 AND rate_cents = 0`,
		request.LineID, request.RunID)
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
