package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// maxTaskComment bounds one line of a task's feed (TASK-02).
const maxTaskComment = 2000

// AddTaskComment appends a line to a task's feed. Any member may write there; an
// archived task is read-only, as the rest of the task is (TASK-02).
func (d *DB) AddTaskComment(ctx context.Context, request appmodel.TaskCommentRequest) (appmodel.TaskComment, error) {
	if request.TeamID <= 0 || request.TaskID <= 0 || request.CallerID <= 0 {
		return appmodel.TaskComment{}, ErrNotFound
	}
	body := strings.TrimSpace(request.Body)
	if body == "" || len([]rune(body)) > maxTaskComment {
		return appmodel.TaskComment{}, appmodel.ErrInvalidTask
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return appmodel.TaskComment{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamRole(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return appmodel.TaskComment{}, err
	}
	if _, err := taskProject(ctx, tx, request.TeamID, request.TaskID); err != nil {
		return appmodel.TaskComment{}, err
	}
	now := FormatTime(d.currentTime().UTC())
	comment := appmodel.TaskComment{TaskID: request.TaskID}
	var created string
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO task_comments (team_id, task_id, user_id, body, created_at) VALUES (?, ?, ?, ?, ?) RETURNING id, created_at`,
		request.TeamID, request.TaskID, request.CallerID, body, now).Scan(&comment.ID, &created); err != nil {
		return appmodel.TaskComment{}, fmt.Errorf("insert task comment: %w", err)
	}
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(NULLIF(name, ''), email) FROM users WHERE id = ?`, request.CallerID).Scan(&comment.Author); err != nil {
		return appmodel.TaskComment{}, err
	}
	comment.Body = body
	if comment.CreatedAt, err = ScanTime(created); err != nil {
		return appmodel.TaskComment{}, err
	}
	if err := tx.Commit(); err != nil {
		return appmodel.TaskComment{}, err
	}
	return comment, nil
}

// ListTaskComments returns a task's feed in the order it was written.
func (d *DB) ListTaskComments(ctx context.Context, teamID, taskID int64) ([]appmodel.TaskComment, error) {
	if teamID <= 0 || taskID <= 0 {
		return nil, ErrNotFound
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT c.id, c.task_id, COALESCE(NULLIF(u.name, ''), u.email), c.body, c.created_at
		 FROM task_comments c JOIN users u ON u.id = c.user_id
		 WHERE c.team_id = ? AND c.task_id = ? ORDER BY c.id`, teamID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []appmodel.TaskComment
	for rows.Next() {
		var c appmodel.TaskComment
		var created string
		if err := rows.Scan(&c.ID, &c.TaskID, &c.Author, &c.Body, &created); err != nil {
			return nil, err
		}
		if c.CreatedAt, err = ScanTime(created); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
