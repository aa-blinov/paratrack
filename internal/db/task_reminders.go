package db

import (
	"context"
	"errors"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// TasksDueOn returns the open, assigned tasks due on or before the given day,
// each with the marker of the last due-date reminder it got (TASK-05). Archived
// and finished tasks are left out, so nothing is nudged about them.
func (d *DB) TasksDueOn(ctx context.Context, through string) ([]appmodel.DueTask, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, team_id, title, due_date, assignee_user_id, COALESCE(due_reminder, '')
		   FROM tasks
		  WHERE archived = 0 AND status <> ? AND assignee_user_id > 0
		    AND due_date IS NOT NULL AND due_date <> '' AND due_date <= ?
		  ORDER BY team_id, id`,
		model.TaskStatusDone, through)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []appmodel.DueTask
	for rows.Next() {
		var task appmodel.DueTask
		if err := rows.Scan(&task.TaskID, &task.TeamID, &task.Title, &task.Due, &task.AssigneeID, &task.Reminded); err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, rows.Err()
}

// MarkTaskDueReminded stores the marker of the reminder a task just got, so the
// same reminder for the same due date is not sent twice.
func (d *DB) MarkTaskDueReminded(ctx context.Context, request appmodel.TaskDueReminderRequest) error {
	if request.TeamID <= 0 || request.TaskID <= 0 {
		return errors.New("task due reminder needs a workspace and a task")
	}
	_, err := d.sql.ExecContext(ctx,
		`UPDATE tasks SET due_reminder = ? WHERE id = ? AND team_id = ?`,
		request.Marker, request.TaskID, request.TeamID)
	return err
}
