package db

import (
	"context"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// AddTaskDependency makes a task wait for another in the same project. A task cannot wait for
// itself, and no chain of waits may loop back: the new link is refused when the task it would
// wait for already waits, directly or through others, for the waiting task (TASK-11).
func (d *DB) AddTaskDependency(ctx context.Context, request appmodel.TaskDependencyRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.TaskID <= 0 || request.DependsOnID <= 0 {
		return ErrNotFound
	}
	if request.TaskID == request.DependsOnID {
		return appmodel.ErrInvalidTask
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamRole(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return err
	}
	projectID, err := taskProject(ctx, tx, request.TeamID, request.TaskID)
	if err != nil {
		return err
	}
	otherProject, err := taskProject(ctx, tx, request.TeamID, request.DependsOnID)
	if err != nil {
		return err
	}
	if otherProject != projectID {
		return appmodel.ErrInvalidTask
	}
	var loops int
	if err := tx.QueryRowContext(ctx, `
		WITH RECURSIVE waits(id) AS (
		    SELECT depends_on_id FROM task_dependencies WHERE task_id = ? AND team_id = ?
		    UNION
		    SELECT d.depends_on_id FROM task_dependencies d JOIN waits w ON d.task_id = w.id WHERE d.team_id = ?
		)
		SELECT COUNT(*) FROM waits WHERE id = ?`,
		request.DependsOnID, request.TeamID, request.TeamID, request.TaskID).Scan(&loops); err != nil {
		return err
	}
	if loops > 0 {
		return appmodel.ErrInvalidTask
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO task_dependencies (team_id, task_id, depends_on_id, created_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT DO NOTHING`,
		request.TeamID, request.TaskID, request.DependsOnID, FormatTime(d.currentTime().UTC())); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteTaskDependency removes a link: the task no longer waits for the other.
func (d *DB) DeleteTaskDependency(ctx context.Context, request appmodel.TaskDependencyRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 {
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
	res, err := tx.ExecContext(ctx,
		`DELETE FROM task_dependencies WHERE team_id = ? AND task_id = ? AND depends_on_id = ?`,
		request.TeamID, request.TaskID, request.DependsOnID)
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

// TaskDependencies returns what a task waits for, and what it blocks.
func (d *DB) TaskDependencies(ctx context.Context, teamID, taskID int64) (waits []appmodel.TaskLink, blocks []appmodel.TaskLink, err error) {
	if teamID <= 0 || taskID <= 0 {
		return nil, nil, ErrNotFound
	}
	waits, err = d.taskLinks(ctx,
		`SELECT t.id, t.title, t.status FROM task_dependencies d JOIN tasks t ON t.id = d.depends_on_id
		 WHERE d.team_id = ? AND d.task_id = ? ORDER BY t.id`, teamID, taskID)
	if err != nil {
		return nil, nil, err
	}
	blocks, err = d.taskLinks(ctx,
		`SELECT t.id, t.title, t.status FROM task_dependencies d JOIN tasks t ON t.id = d.task_id
		 WHERE d.team_id = ? AND d.depends_on_id = ? ORDER BY t.id`, teamID, taskID)
	if err != nil {
		return nil, nil, err
	}
	return waits, blocks, nil
}

func (d *DB) taskLinks(ctx context.Context, query string, args ...any) ([]appmodel.TaskLink, error) {
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []appmodel.TaskLink{}
	for rows.Next() {
		var link appmodel.TaskLink
		var status string
		if err := rows.Scan(&link.ID, &link.Title, &status); err != nil {
			return nil, err
		}
		link.Done = status == "done"
		out = append(out, link)
	}
	return out, rows.Err()
}
