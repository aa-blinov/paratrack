package db

import (
	"context"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// Roles a person can have on a task (TASK-12).
const (
	taskRoleWatcher    = "watcher"
	taskRoleCoAssignee = "co-assignee"
)

// SetTaskWatcher makes a person follow a task, or stops it. A member may follow or stop following
// the task themselves, as a watcher. A manager adds anyone, and may name a co-assignee (TASK-12).
func (d *DB) SetTaskWatcher(ctx context.Context, request appmodel.TaskWatchRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.TaskID <= 0 || request.UserID <= 0 {
		return ErrNotFound
	}
	if request.Role != taskRoleWatcher && request.Role != taskRoleCoAssignee {
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
	if _, err := taskProject(ctx, tx, request.TeamID, request.TaskID); err != nil {
		return err
	}
	if request.UserID != request.CallerID || request.Role == taskRoleCoAssignee {
		if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
			return err
		}
	}
	if err := checkMemberInTeam(ctx, tx, request.UserID, request.TeamID); err != nil {
		return err
	}
	if !request.Watch {
		if _, err := tx.ExecContext(ctx, `DELETE FROM task_watchers WHERE task_id = ? AND user_id = ? AND team_id = ?`,
			request.TaskID, request.UserID, request.TeamID); err != nil {
			return err
		}
		return tx.Commit()
	}
	// A member who follows a task does not take away a co-assignee's role; a manager sets it.
	conflict := `DO NOTHING`
	if request.UserID != request.CallerID || request.Role == taskRoleCoAssignee {
		conflict = `DO UPDATE SET role = excluded.role`
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO task_watchers (team_id, task_id, user_id, role, created_at) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (task_id, user_id) `+conflict,
		request.TeamID, request.TaskID, request.UserID, request.Role, FormatTime(d.currentTime().UTC())); err != nil {
		return err
	}
	return tx.Commit()
}

// ListTaskWatchers lists who follows a task, with the role each has on it.
func (d *DB) ListTaskWatchers(ctx context.Context, teamID, taskID int64) ([]appmodel.TaskWatcher, error) {
	if teamID <= 0 || taskID <= 0 {
		return nil, ErrNotFound
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT w.user_id, COALESCE(NULLIF(u.name, ''), u.email), w.role
		   FROM task_watchers w JOIN users u ON u.id = w.user_id
		  WHERE w.team_id = ? AND w.task_id = ? ORDER BY w.id`, teamID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []appmodel.TaskWatcher{}
	for rows.Next() {
		var w appmodel.TaskWatcher
		if err := rows.Scan(&w.UserID, &w.Name, &w.Role); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// TaskWatcherIDs lists the people who follow a task, for a notification.
func (d *DB) TaskWatcherIDs(ctx context.Context, teamID, taskID int64) ([]int64, error) {
	if teamID <= 0 || taskID <= 0 {
		return nil, ErrNotFound
	}
	rows, err := d.sql.QueryContext(ctx, `SELECT user_id FROM task_watchers WHERE team_id = ? AND task_id = ? ORDER BY id`, teamID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
