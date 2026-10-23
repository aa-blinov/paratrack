package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// maxTaskChecklistItem bounds one step of a task's checklist (TASK-10).
const maxTaskChecklistItem = 300

// AddChecklistItem appends a step to a task's checklist. Any member may add a step.
func (d *DB) AddChecklistItem(ctx context.Context, request appmodel.TaskChecklistAddRequest) (appmodel.TaskChecklistItem, error) {
	if request.TeamID <= 0 || request.TaskID <= 0 || request.CallerID <= 0 {
		return appmodel.TaskChecklistItem{}, ErrNotFound
	}
	body := strings.TrimSpace(request.Body)
	if body == "" || len([]rune(body)) > maxTaskChecklistItem {
		return appmodel.TaskChecklistItem{}, appmodel.ErrInvalidTask
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return appmodel.TaskChecklistItem{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamRole(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return appmodel.TaskChecklistItem{}, err
	}
	if _, err := taskProject(ctx, tx, request.TeamID, request.TaskID); err != nil {
		return appmodel.TaskChecklistItem{}, err
	}
	var position int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(position), 0) + 1 FROM task_checklist_items WHERE team_id = ? AND task_id = ?`,
		request.TeamID, request.TaskID).Scan(&position); err != nil {
		return appmodel.TaskChecklistItem{}, err
	}
	item := appmodel.TaskChecklistItem{TaskID: request.TaskID, Body: body}
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO task_checklist_items (team_id, task_id, body, done, position, created_at) VALUES (?, ?, ?, 0, ?, ?) RETURNING id`,
		request.TeamID, request.TaskID, body, position, FormatTime(d.currentTime().UTC())).Scan(&item.ID); err != nil {
		return appmodel.TaskChecklistItem{}, fmt.Errorf("insert checklist item: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return appmodel.TaskChecklistItem{}, err
	}
	return item, nil
}

// SetChecklistItem marks a step done or not again.
func (d *DB) SetChecklistItem(ctx context.Context, request appmodel.TaskChecklistSetRequest) error {
	return d.checklistWrite(ctx, request.TeamID, request.CallerID,
		`UPDATE task_checklist_items SET done = ? WHERE id = ? AND task_id = ? AND team_id = ?`,
		boolInt(request.Done), request.ItemID, request.TaskID, request.TeamID)
}

// DeleteChecklistItem removes one step of a task's checklist.
func (d *DB) DeleteChecklistItem(ctx context.Context, request appmodel.TaskChecklistDeleteRequest) error {
	return d.checklistWrite(ctx, request.TeamID, request.CallerID,
		`DELETE FROM task_checklist_items WHERE id = ? AND task_id = ? AND team_id = ?`,
		request.ItemID, request.TaskID, request.TeamID)
}

// checklistWrite runs one change to a step under the team's role lock, and reports a step
// that is not in this team's task as not found.
func (d *DB) checklistWrite(ctx context.Context, teamID, callerID int64, query string, args ...any) error {
	if teamID <= 0 || callerID <= 0 {
		return ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamRole(ctx, tx, teamID, callerID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, query, args...)
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

// ListChecklist returns a task's steps in the order they were added.
func (d *DB) ListChecklist(ctx context.Context, teamID, taskID int64) ([]appmodel.TaskChecklistItem, error) {
	if teamID <= 0 || taskID <= 0 {
		return nil, ErrNotFound
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, task_id, body, done FROM task_checklist_items WHERE team_id = ? AND task_id = ? ORDER BY position, id`,
		teamID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []appmodel.TaskChecklistItem
	for rows.Next() {
		var item appmodel.TaskChecklistItem
		var done int64
		if err := rows.Scan(&item.ID, &item.TaskID, &item.Body, &done); err != nil {
			return nil, err
		}
		item.Done = done != 0
		out = append(out, item)
	}
	return out, rows.Err()
}
