package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// TaskOfActivity returns the task an activity belongs to, or zero when it belongs
// to none (TIME-04).
func (d *DB) TaskOfActivity(ctx context.Context, teamID, activityID int64) (int64, error) {
	if teamID <= 0 || activityID <= 0 {
		return 0, ErrNotFound
	}
	var taskID sql.NullInt64
	err := d.sql.QueryRowContext(ctx,
		`SELECT task_id FROM activities WHERE id = ? AND team_id = ?`, activityID, teamID).Scan(&taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	return taskID.Int64, nil
}

// StartTaskWork moves a task from "to do" to "in progress" when time starts on it,
// at the end of that column. A task already under way, done, or archived is left as
// it is (TIME-04).
func (d *DB) StartTaskWork(ctx context.Context, request appmodel.TaskStartRequest) error {
	if request.TeamID <= 0 || request.TaskID <= 0 || request.CallerID <= 0 {
		return ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamRole(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return err
	}
	now := FormatTime(d.currentTime().UTC())
	if _, err := tx.ExecContext(ctx,
		`UPDATE tasks SET status = ?, position = (SELECT COALESCE(MAX(position), 0) + 1 FROM tasks WHERE team_id = ? AND status = ?), updated_at = ?
		 WHERE id = ? AND team_id = ? AND status = ? AND archived = 0`,
		model.TaskStatusDoing, request.TeamID, model.TaskStatusDoing, now,
		request.TaskID, request.TeamID, model.TaskStatusTodo); err != nil {
		return fmt.Errorf("start task work: %w", err)
	}
	return tx.Commit()
}
