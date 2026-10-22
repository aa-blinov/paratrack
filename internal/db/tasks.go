package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
)

const taskSelect = `SELECT t.id, t.team_id, t.project_id, t.title, t.status, t.position,
	COALESCE(t.assignee_user_id, 0), COALESCE(t.due_date, ''), COALESCE(t.estimate_minutes, 0),
	COALESCE(t.created_by, 0), t.archived, COALESCE(t.external_url, ''), t.created_at, t.updated_at, p.name, p.color
	FROM tasks t JOIN projects p ON p.id = t.project_id`

// insertTaskTx writes a task at the end of its column, with its first activity.
// The caller owns the transaction and has checked the project and the actor.
func insertTaskTx(ctx context.Context, tx *Tx, request appmodel.TaskCreateRequest, now string) (int64, error) {
	var id int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO tasks (team_id, project_id, title, status, position, assignee_user_id, due_date, estimate_minutes, created_by, archived, created_at, updated_at, external_ref, external_url)
		 VALUES (?, ?, ?, ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM tasks WHERE team_id = ? AND status = ?), ?, ?, ?, ?, 0, ?, ?, ?, ?)
		 RETURNING id`,
		request.TeamID, request.ProjectID, request.Title, request.Status, request.TeamID, request.Status,
		nullIfZero(request.AssigneeID), nullIfEmpty(request.Due), nullIfZero(int64(request.EstimateMinutes)), request.CallerID, now, now,
		nullIfEmpty(request.ExternalRef), nullIfEmpty(request.ExternalURL),
	).Scan(&id); err != nil {
		return 0, fmt.Errorf("insert task: %w", err)
	}
	if err := insertTaskActivity(ctx, tx, request.TeamID, request.ProjectID, id, request.Title, now); err != nil {
		return 0, err
	}
	return id, nil
}

// CreateTask adds a task to a project. It starts with one activity of the same
// name, so the timer has something to write to from the first minute.
func (d *DB) CreateTask(ctx context.Context, request appmodel.TaskCreateRequest) (model.Task, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.ProjectID <= 0 {
		return model.Task{}, ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.Task{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamRole(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return model.Task{}, err
	}
	if err := requireProjectInTeam(ctx, tx, request.ProjectID, request.TeamID); err != nil {
		return model.Task{}, err
	}
	if request.AssigneeID > 0 {
		if err := checkMemberInTeam(ctx, tx, request.AssigneeID, request.TeamID); err != nil {
			return model.Task{}, err
		}
	}
	id, err := insertTaskTx(ctx, tx, request, FormatTime(d.currentTime().UTC()))
	if err != nil {
		return model.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Task{}, err
	}
	return d.taskByID(ctx, request.TeamID, id)
}

// UpdateTask changes the fields a form sent. The project is fixed at creation:
// a task moves between projects only by being recreated.
func (d *DB) UpdateTask(ctx context.Context, request appmodel.TaskUpdateRequest) (model.Task, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.TaskID <= 0 {
		return model.Task{}, ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.Task{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamRole(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return model.Task{}, err
	}
	if _, err := taskProject(ctx, tx, request.TeamID, request.TaskID); err != nil {
		return model.Task{}, err
	}
	if request.Title != nil {
		title := strings.TrimSpace(*request.Title)
		if title == "" {
			return model.Task{}, appmodel.ErrInvalidTask
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET title = ? WHERE id = ? AND team_id = ?`, title, request.TaskID, request.TeamID); err != nil {
			return model.Task{}, err
		}
	}
	if request.AssigneeID != nil {
		if *request.AssigneeID > 0 {
			if err := checkMemberInTeam(ctx, tx, *request.AssigneeID, request.TeamID); err != nil {
				return model.Task{}, err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET assignee_user_id = ? WHERE id = ? AND team_id = ?`, nullIfZero(*request.AssigneeID), request.TaskID, request.TeamID); err != nil {
			return model.Task{}, err
		}
	}
	if request.Due != nil {
		if *request.Due != "" {
			if err := checkProjectDeadline(*request.Due); err != nil {
				return model.Task{}, appmodel.ErrInvalidTask
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET due_date = ? WHERE id = ? AND team_id = ?`, nullIfEmpty(*request.Due), request.TaskID, request.TeamID); err != nil {
			return model.Task{}, err
		}
	}
	if request.EstimateMinutes != nil {
		if *request.EstimateMinutes < 0 {
			return model.Task{}, appmodel.ErrInvalidTask
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET estimate_minutes = ? WHERE id = ? AND team_id = ?`, nullIfZero(int64(*request.EstimateMinutes)), request.TaskID, request.TeamID); err != nil {
			return model.Task{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET updated_at = ? WHERE id = ? AND team_id = ?`, FormatTime(d.currentTime().UTC()), request.TaskID, request.TeamID); err != nil {
		return model.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Task{}, err
	}
	return d.taskByID(ctx, request.TeamID, request.TaskID)
}

// SetTaskStatus moves a task to another board column, at the end of it.
func (d *DB) SetTaskStatus(ctx context.Context, request appmodel.TaskStatusRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.TaskID <= 0 {
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
	if _, err := taskProject(ctx, tx, request.TeamID, request.TaskID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE tasks SET status = ?, position = (SELECT COALESCE(MAX(position), 0) + 1 FROM tasks WHERE team_id = ? AND status = ?), updated_at = ?
		 WHERE id = ? AND team_id = ?`,
		request.Status, request.TeamID, request.Status, FormatTime(d.currentTime().UTC()), request.TaskID, request.TeamID); err != nil {
		return err
	}
	return tx.Commit()
}

// ArchiveTask takes a task off the board. Its activities and the time they
// logged stay, and the timers running on those activities are stopped now.
// A manager or the person who created the task may do it.
func (d *DB) ArchiveTask(ctx context.Context, request appmodel.TaskArchiveRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.TaskID <= 0 {
		return ErrNotFound
	}
	if err := d.archiveTaskRow(ctx, request); err != nil {
		return err
	}
	// Once the task is archived no session can start on its activities, so this
	// closes every timer that was running. Each stop is the ordinary one, with
	// the approval lock and the webhook event.
	now := d.currentTime()
	open, err := d.openSessionIDsForTask(ctx, request.TeamID, request.TaskID)
	if err != nil {
		return err
	}
	for _, id := range open {
		if _, err := d.UpdateSessionEnd(ctx, appmodel.TimerStopRequest{TeamID: request.TeamID, SessionID: id, At: now}); err != nil && !errors.Is(err, ErrSessionNotActive) {
			return err
		}
	}
	return nil
}

// archiveTaskRow checks who may archive the task and marks it archived. It locks
// the task's activities first: a timer starting on one of them waits for this
// commit and is then refused, so none can slip in between.
func (d *DB) archiveTaskRow(ctx context.Context, request appmodel.TaskArchiveRequest) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, role, err := lockTeamRole(ctx, tx, request.TeamID, request.CallerID)
	if err != nil {
		return err
	}
	var createdBy int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(created_by, 0) FROM tasks WHERE id = ? AND team_id = ?`, request.TaskID, request.TeamID).Scan(&createdBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return appmodel.ErrTaskNotFound
		}
		return err
	}
	if !role.CanManage() && createdBy != request.CallerID {
		return model.ErrForbidden
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM activities WHERE task_id = ? AND team_id = ? ORDER BY id FOR UPDATE`, request.TaskID, request.TeamID)
	if err != nil {
		return err
	}
	for rows.Next() {
		// The rows are only read to take their locks.
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET archived = 1, updated_at = ? WHERE id = ? AND team_id = ?`,
		FormatTime(d.currentTime().UTC()), request.TaskID, request.TeamID); err != nil {
		return err
	}
	return tx.Commit()
}

// openSessionIDsForTask lists the sessions still running on a task's activities,
// whoever started them.
func (d *DB) openSessionIDsForTask(ctx context.Context, teamID, taskID int64) ([]int64, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT s.id FROM sessions s JOIN activities a ON a.id = s.activity_id
		 WHERE a.task_id = ? AND s.team_id = ? AND s.end_at IS NULL ORDER BY s.id`, taskID, teamID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// requireActivityOpenForTime refuses new time on an activity that an archived
// task holds: the task is read-only until it is put back on the board.
func requireActivityOpenForTime(ctx context.Context, tx *Tx, activityID int64) error {
	var archived int
	err := tx.QueryRowContext(ctx,
		`SELECT t.archived FROM activities a JOIN tasks t ON t.id = a.task_id WHERE a.id = ?`, activityID).Scan(&archived)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if archived != 0 {
		return appmodel.ErrTaskArchived
	}
	return nil
}

// RestoreTask puts an archived task back on the board, at the end of its column.
// Who may do it is the same as who may archive it: a manager or the creator.
func (d *DB) RestoreTask(ctx context.Context, request appmodel.TaskRestoreRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.TaskID <= 0 {
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
	var createdBy int64
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(created_by, 0), status FROM tasks WHERE id = ? AND team_id = ?`, request.TaskID, request.TeamID).Scan(&createdBy, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return appmodel.ErrTaskNotFound
		}
		return err
	}
	if !role.CanManage() && createdBy != request.CallerID {
		return model.ErrForbidden
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE tasks SET archived = 0, position = (SELECT COALESCE(MAX(position), 0) + 1 FROM tasks WHERE team_id = ? AND status = ?), updated_at = ?
		 WHERE id = ? AND team_id = ?`,
		request.TeamID, status, FormatTime(d.currentTime().UTC()), request.TaskID, request.TeamID); err != nil {
		return err
	}
	return tx.Commit()
}

// LinkTaskActivity hangs an activity on a task: an existing one from the same
// project, or a new one with the given name. An activity holds one task at most.
func (d *DB) LinkTaskActivity(ctx context.Context, request appmodel.TaskActivityRequest) (appmodel.Activity, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.TaskID <= 0 {
		return appmodel.Activity{}, ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return appmodel.Activity{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamRole(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return appmodel.Activity{}, err
	}
	projectID, err := taskProject(ctx, tx, request.TeamID, request.TaskID)
	if err != nil {
		return appmodel.Activity{}, err
	}
	now := FormatTime(d.currentTime().UTC())
	var activityID int64
	if request.ActivityID > 0 {
		var current sql.NullInt64
		var activityProject sql.NullInt64
		if err := tx.QueryRowContext(ctx,
			`SELECT task_id, project_id FROM activities WHERE id = ? AND team_id = ?`, request.ActivityID, request.TeamID).Scan(&current, &activityProject); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return appmodel.Activity{}, ErrNotFound
			}
			return appmodel.Activity{}, err
		}
		if current.Valid && current.Int64 != request.TaskID {
			return appmodel.Activity{}, appmodel.ErrTaskActivityTaken
		}
		if !activityProject.Valid || activityProject.Int64 != projectID {
			return appmodel.Activity{}, appmodel.ErrInvalidTask
		}
		if _, err := tx.ExecContext(ctx, `UPDATE activities SET task_id = ?, updated_at = ? WHERE id = ? AND team_id = ?`,
			request.TaskID, now, request.ActivityID, request.TeamID); err != nil {
			return appmodel.Activity{}, err
		}
		activityID = request.ActivityID
	} else {
		name := strings.TrimSpace(request.Name)
		if name == "" {
			return appmodel.Activity{}, appmodel.ErrInvalidTask
		}
		if activityID, err = insertTaskActivityRow(ctx, tx, request.TeamID, projectID, request.TaskID, name, now); err != nil {
			return appmodel.Activity{}, err
		}
	}
	var out appmodel.Activity
	if err := tx.QueryRowContext(ctx, `SELECT id, name, color FROM activities WHERE id = ?`, activityID).Scan(&out.ID, &out.Name, &out.Color); err != nil {
		return appmodel.Activity{}, err
	}
	if err := tx.Commit(); err != nil {
		return appmodel.Activity{}, err
	}
	return out, nil
}

// ListTasks returns the open board of a workspace, or its archive, optionally
// for one project, or one task whether it is archived or not. Each row carries
// the task's activities and the time logged on them.
func (d *DB) ListTasks(ctx context.Context, query appmodel.TaskQuery) ([]appmodel.TaskRow, error) {
	if query.TeamID <= 0 {
		return nil, ErrNotFound
	}
	args := []any{query.TeamID}
	where := ` WHERE t.team_id = ?`
	switch {
	case query.TaskID > 0:
		where += ` AND t.id = ?`
		args = append(args, query.TaskID)
	case query.Archived:
		where += ` AND t.archived = 1`
	default:
		where += ` AND t.archived = 0`
	}
	if query.ProjectID > 0 {
		where += ` AND t.project_id = ?`
		args = append(args, query.ProjectID)
	}
	rows, err := d.sql.QueryContext(ctx, taskSelect+where+` ORDER BY t.position, t.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	var out []appmodel.TaskRow
	index := map[int64]int{}
	for rows.Next() {
		task, projectName, projectColor, err := scanTaskRow(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		index[task.ID] = len(out)
		out = append(out, appmodel.TaskRow{Task: task, ProjectName: projectName, ProjectColor: projectColor})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	if len(out) == 0 {
		return out, nil
	}
	if err := d.attachTaskActivities(ctx, query.TeamID, out, index); err != nil {
		return nil, err
	}
	tracked, err := d.trackedMinutesByTask(ctx, query.TeamID, query.Now)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].TrackedMin = tracked[out[i].Task.ID]
	}
	return out, nil
}

func (d *DB) taskByID(ctx context.Context, teamID, taskID int64) (model.Task, error) {
	rows, err := d.ListTasks(ctx, appmodel.TaskQuery{TeamID: teamID, Now: d.currentTime()})
	if err != nil {
		return model.Task{}, err
	}
	for _, row := range rows {
		if row.Task.ID == taskID {
			return row.Task, nil
		}
	}
	return model.Task{}, appmodel.ErrTaskNotFound
}

func (d *DB) attachTaskActivities(ctx context.Context, teamID int64, out []appmodel.TaskRow, index map[int64]int) error {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, name, color, task_id FROM activities WHERE team_id = ? AND task_id IS NOT NULL AND archived = 0 ORDER BY id`, teamID)
	if err != nil {
		return fmt.Errorf("list task activities: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var a appmodel.Activity
		var taskID int64
		if err := rows.Scan(&a.ID, &a.Name, &a.Color, &taskID); err != nil {
			return err
		}
		if i, ok := index[taskID]; ok {
			out[i].Activities = append(out[i].Activities, a)
		}
	}
	return rows.Err()
}

// trackedMinutesByTask sums what the sessions of each task's activities logged,
// open timers included, the same way the timesheet counts them.
func (d *DB) trackedMinutesByTask(ctx context.Context, teamID int64, now time.Time) (map[int64]int, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT a.task_id, s.start_at, s.end_at, s.accumulated_seconds, s.paused, s.last_resume_at
		 FROM sessions s JOIN activities a ON a.id = s.activity_id AND a.team_id = s.team_id
		 WHERE s.team_id = ? AND a.task_id IS NOT NULL`, teamID)
	if err != nil {
		return nil, fmt.Errorf("read task sessions: %w", err)
	}
	defer rows.Close()
	seconds := map[int64]int{}
	from := time.Unix(0, 0).UTC()
	until := now.AddDate(100, 0, 0)
	for rows.Next() {
		var taskID int64
		var startAt string
		var endAt, lastResumeAt sql.NullString
		var accumulated, paused int
		if err := rows.Scan(&taskID, &startAt, &endAt, &accumulated, &paused, &lastResumeAt); err != nil {
			return nil, err
		}
		session, err := scheduleSession(startAt, endAt, lastResumeAt, accumulated, paused)
		if err != nil {
			return nil, err
		}
		total, err := money.AddInt(seconds[taskID], session.TrackedSecondsInWindow(from, until, now))
		if err != nil {
			return nil, err
		}
		seconds[taskID] = total
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	minutes := make(map[int64]int, len(seconds))
	for id, sec := range seconds {
		minutes[id] = sec / 60
	}
	return minutes, nil
}

func scanTaskRow(r row) (model.Task, string, string, error) {
	var (
		t                  model.Task
		archived           int
		created, updated   string
		projectName, color string
	)
	if err := r.Scan(&t.ID, &t.TeamID, &t.ProjectID, &t.Title, &t.Status, &t.Position, &t.AssigneeID, &t.Due,
		&t.EstimateMinutes, &t.CreatedBy, &archived, &t.ExternalURL, &created, &updated, &projectName, &color); err != nil {
		return model.Task{}, "", "", err
	}
	t.Archived = archived != 0
	var err error
	if t.CreatedAt, err = ScanTime(created); err != nil {
		return model.Task{}, "", "", fmt.Errorf("parse task creation time: %w", err)
	}
	if t.UpdatedAt, err = ScanTime(updated); err != nil {
		return model.Task{}, "", "", fmt.Errorf("parse task update time: %w", err)
	}
	return t, projectName, color, nil
}

// taskProject returns the project of an open task. An archived task is read-only
// until it is restored, so an edit to it is refused as archived, not as missing.
func taskProject(ctx context.Context, tx *Tx, teamID, taskID int64) (int64, error) {
	var projectID int64
	var archived int
	err := tx.QueryRowContext(ctx, `SELECT project_id, archived FROM tasks WHERE id = ? AND team_id = ?`, taskID, teamID).Scan(&projectID, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, appmodel.ErrTaskNotFound
	}
	if err != nil {
		return 0, err
	}
	if archived != 0 {
		return 0, appmodel.ErrTaskArchived
	}
	return projectID, nil
}

func requireProjectInTeam(ctx context.Context, tx *Tx, projectID, teamID int64) error {
	var one int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE id = ? AND team_id = ?`, projectID, teamID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func insertTaskActivity(ctx context.Context, tx *Tx, teamID, projectID, taskID int64, name, now string) error {
	_, err := insertTaskActivityRow(ctx, tx, teamID, projectID, taskID, name, now)
	return err
}

// insertTaskActivityRow writes an activity for a task and gives it its colour,
// the same way every other team activity gets one.
func insertTaskActivityRow(ctx context.Context, tx *Tx, teamID, projectID, taskID int64, name, now string) (int64, error) {
	var id int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO activities (name, name_key, team_id, project_id, task_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		name, strings.ToLower(name), teamID, projectID, taskID, now, now).Scan(&id); err != nil {
		return 0, fmt.Errorf("insert task activity: %w", err)
	}
	if err := assignActivityColorTx(ctx, tx, id, teamID); err != nil {
		return 0, err
	}
	return id, nil
}

// ListLinkableActivities lists the project's activities that no task holds yet:
// the ones a task can take on.
// ActivityHasTask reports whether an activity is hung on a task. Time logged to
// an activity without one is kept, but it is on no task.
func (d *DB) ActivityHasTask(ctx context.Context, teamID, activityID int64) (bool, error) {
	var linked bool
	err := d.sql.QueryRowContext(ctx,
		`SELECT task_id IS NOT NULL AND task_id > 0 FROM activities WHERE id = ? AND team_id = ?`,
		activityID, teamID).Scan(&linked)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return linked, err
}

func (d *DB) ListLinkableActivities(ctx context.Context, query appmodel.TaskQuery) ([]appmodel.Activity, error) {
	if query.TeamID <= 0 || query.ProjectID <= 0 {
		return nil, ErrNotFound
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, name, color FROM activities
		 WHERE team_id = ? AND project_id = ? AND task_id IS NULL AND archived = 0 ORDER BY name`,
		query.TeamID, query.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("list linkable activities: %w", err)
	}
	defer rows.Close()
	var out []appmodel.Activity
	for rows.Next() {
		var a appmodel.Activity
		if err := rows.Scan(&a.ID, &a.Name, &a.Color); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
