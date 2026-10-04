package db

import (
	"context"
	"database/sql"
)

type contextExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// execRequireRows executes a scoped write that must match an existing row.
// It preserves database and driver errors and reports a missing row distinctly.
func execRequireRows(ctx context.Context, execer contextExecer, query string, args ...any) error {
	result, err := execer.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return ErrNotFound
	}
	return nil
}

// execManagerTeamUpdate serializes a manager-only workspace setting change
// with role changes and member removal, then requires the team row to exist.
func (d *DB) execManagerTeamUpdate(ctx context.Context, teamID, callerID int64, query string, args ...any) error {
	if teamID <= 0 || callerID <= 0 {
		return ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return err
	}
	if err := execRequireRows(ctx, tx, query, args...); err != nil {
		return err
	}
	return tx.Commit()
}
