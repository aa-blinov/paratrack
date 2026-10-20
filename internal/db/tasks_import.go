package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// ImportTasks writes board items as tasks of one project. An item already imported
// (the same reference) is updated in place: its title and column follow the board.
// An archived task stays as it is. Only managers may import, and the whole import
// is one transaction, so a failure leaves the workspace as it was.
func (d *DB) ImportTasks(ctx context.Context, request appmodel.TaskImportRequest) (appmodel.TaskImportResult, error) {
	var out appmodel.TaskImportResult
	if request.TeamID <= 0 || request.CallerID <= 0 || request.ProjectID <= 0 {
		return out, ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return out, err
	}
	if err := requireProjectInTeam(ctx, tx, request.ProjectID, request.TeamID); err != nil {
		return out, err
	}
	now := FormatTime(d.currentTime().UTC())
	for _, item := range request.Items {
		var id int64
		var archived int
		var title, status string
		err := tx.QueryRowContext(ctx,
			`SELECT id, archived, title, status FROM tasks WHERE team_id = ? AND external_ref = ?`,
			request.TeamID, item.Ref).Scan(&id, &archived, &title, &status)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if _, err := insertTaskTx(ctx, tx, appmodel.TaskCreateRequest{
				TeamID: request.TeamID, CallerID: request.CallerID, ProjectID: request.ProjectID,
				Title: item.Title, Status: item.Status, ExternalRef: item.Ref, ExternalURL: item.URL,
			}, now); err != nil {
				return out, err
			}
			out.Created++
		case err != nil:
			return out, err
		case archived != 0:
			out.Archived++
		case title == item.Title && status == item.Status:
			out.Unchanged++
		default:
			if _, err := tx.ExecContext(ctx, `UPDATE tasks SET title = ?, updated_at = ? WHERE id = ?`, item.Title, now, id); err != nil {
				return out, err
			}
			if status != item.Status {
				if _, err := tx.ExecContext(ctx,
					`UPDATE tasks SET status = ?, position = (SELECT COALESCE(MAX(position), 0) + 1 FROM tasks WHERE team_id = ? AND status = ?) WHERE id = ?`,
					item.Status, request.TeamID, item.Status, id); err != nil {
					return out, err
				}
			}
			out.Updated++
		}
	}
	if err := tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}
