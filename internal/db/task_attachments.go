package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// maxTaskAttachmentName bounds the name a file is shown under (TASK-13).
const maxTaskAttachmentName = 200

// AddTaskAttachment stores a file with a task. Any member may attach one; the content type must be
// one the server allows, and the size within the limit (TASK-13).
func (d *DB) AddTaskAttachment(ctx context.Context, request appmodel.TaskAttachmentAddRequest) (appmodel.TaskAttachmentInfo, error) {
	if request.TeamID <= 0 || request.TaskID <= 0 || request.CallerID <= 0 {
		return appmodel.TaskAttachmentInfo{}, ErrNotFound
	}
	name := strings.TrimSpace(request.Name)
	if name == "" || len([]rune(name)) > maxTaskAttachmentName {
		return appmodel.TaskAttachmentInfo{}, appmodel.ErrInvalidTask
	}
	if len(request.Data) == 0 || len(request.Data) > appmodel.MaxTaskAttachmentBytes {
		return appmodel.TaskAttachmentInfo{}, appmodel.ErrTaskAttachmentSize
	}
	if !appmodel.AllowedTaskAttachmentType(request.ContentType) {
		return appmodel.TaskAttachmentInfo{}, appmodel.ErrTaskAttachmentType
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return appmodel.TaskAttachmentInfo{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamRole(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return appmodel.TaskAttachmentInfo{}, err
	}
	if _, err := taskProject(ctx, tx, request.TeamID, request.TaskID); err != nil {
		return appmodel.TaskAttachmentInfo{}, err
	}
	info := appmodel.TaskAttachmentInfo{TaskID: request.TaskID, Name: name, ContentType: request.ContentType, Size: int64(len(request.Data))}
	var created string
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO task_attachments (team_id, task_id, name, content_type, size, data, created_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id, created_at`,
		request.TeamID, request.TaskID, name, request.ContentType, info.Size, request.Data, request.CallerID, FormatTime(d.currentTime().UTC()),
	).Scan(&info.ID, &created); err != nil {
		return appmodel.TaskAttachmentInfo{}, fmt.Errorf("insert task attachment: %w", err)
	}
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(NULLIF(name, ''), email) FROM users WHERE id = ?`, request.CallerID).Scan(&info.Author); err != nil {
		return appmodel.TaskAttachmentInfo{}, err
	}
	if info.CreatedAt, err = ScanTime(created); err != nil {
		return appmodel.TaskAttachmentInfo{}, err
	}
	if err := tx.Commit(); err != nil {
		return appmodel.TaskAttachmentInfo{}, err
	}
	return info, nil
}

// ListTaskAttachments lists a task's files without their bytes, oldest first.
func (d *DB) ListTaskAttachments(ctx context.Context, teamID, taskID int64) ([]appmodel.TaskAttachmentInfo, error) {
	if teamID <= 0 || taskID <= 0 {
		return nil, ErrNotFound
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT a.id, a.task_id, a.name, a.content_type, a.size, COALESCE(NULLIF(u.name, ''), u.email), a.created_at
		   FROM task_attachments a LEFT JOIN users u ON u.id = a.created_by
		  WHERE a.team_id = ? AND a.task_id = ? ORDER BY a.id`, teamID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []appmodel.TaskAttachmentInfo{}
	for rows.Next() {
		var info appmodel.TaskAttachmentInfo
		var created string
		if err := rows.Scan(&info.ID, &info.TaskID, &info.Name, &info.ContentType, &info.Size, &info.Author, &created); err != nil {
			return nil, err
		}
		if info.CreatedAt, err = ScanTime(created); err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, rows.Err()
}

// GetTaskAttachment returns one file with its bytes, for download.
func (d *DB) GetTaskAttachment(ctx context.Context, teamID, taskID, attachmentID int64) (appmodel.TaskAttachmentFile, error) {
	if teamID <= 0 || taskID <= 0 || attachmentID <= 0 {
		return appmodel.TaskAttachmentFile{}, ErrNotFound
	}
	var file appmodel.TaskAttachmentFile
	err := d.sql.QueryRowContext(ctx,
		`SELECT id, task_id, name, content_type, size, data FROM task_attachments
		  WHERE id = ? AND task_id = ? AND team_id = ?`, attachmentID, taskID, teamID,
	).Scan(&file.ID, &file.TaskID, &file.Name, &file.ContentType, &file.Size, &file.Data)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return appmodel.TaskAttachmentFile{}, ErrNotFound
		}
		return appmodel.TaskAttachmentFile{}, err
	}
	return file, nil
}

// DeleteTaskAttachment removes a file. Its author or a manager may do it (TASK-13).
func (d *DB) DeleteTaskAttachment(ctx context.Context, request appmodel.TaskAttachmentDeleteRequest) error {
	if request.TeamID <= 0 || request.TaskID <= 0 || request.CallerID <= 0 || request.AttachmentID <= 0 {
		return ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, role, err := lockTeamRole(ctx, tx, request.TeamID, request.CallerID)
	if err != nil {
		return err
	}
	var author int64
	if err := tx.QueryRowContext(ctx,
		`SELECT created_by FROM task_attachments WHERE id = ? AND task_id = ? AND team_id = ?`,
		request.AttachmentID, request.TaskID, request.TeamID).Scan(&author); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if author != request.CallerID && !role.CanManage() {
		return model.ErrForbidden
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM task_attachments WHERE id = ? AND team_id = ?`, request.AttachmentID, request.TeamID); err != nil {
		return err
	}
	return tx.Commit()
}
