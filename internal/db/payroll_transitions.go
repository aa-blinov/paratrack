package db

import (
	"context"
	"database/sql"
	"errors"

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
