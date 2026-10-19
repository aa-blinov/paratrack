package db

import (
	"context"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// ProjectsDueBetween lists projects whose deadline falls in [from, to] and
// whose reminder for that deadline has not gone out yet, across all teams.
func (d *DB) ProjectsDueBetween(ctx context.Context, from, to string) ([]appmodel.DueProject, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT team_id, id, name, slug, deadline, COALESCE(lead_user_id, 0)
		   FROM projects
		  WHERE archived = 0 AND deadline IS NOT NULL AND deadline >= ? AND deadline <= ?
		    AND (deadline_reminded IS NULL OR deadline_reminded <> deadline)
		  ORDER BY deadline, id`, from, to)
	if err != nil {
		return nil, fmt.Errorf("list due projects: %w", err)
	}
	defer rows.Close()
	var due []appmodel.DueProject
	for rows.Next() {
		var p appmodel.DueProject
		if err := rows.Scan(&p.TeamID, &p.ProjectID, &p.Name, &p.Slug, &p.Deadline, &p.LeadUserID); err != nil {
			return nil, err
		}
		due = append(due, p)
	}
	return due, rows.Err()
}

// MarkDeadlineReminded records that the reminder for this deadline went out.
func (d *DB) MarkDeadlineReminded(ctx context.Context, request appmodel.DeadlineReminderRequest) error {
	_, err := d.sql.ExecContext(ctx,
		`UPDATE projects SET deadline_reminded = ? WHERE id = ? AND team_id = ?`, request.Deadline, request.ProjectID, request.TeamID)
	return err
}

// ManagerIDs lists the owner and the administrators of a workspace.
func (d *DB) ManagerIDs(ctx context.Context, teamID int64) ([]int64, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT user_id FROM memberships WHERE team_id = ? AND role IN ('owner', 'admin')
		 UNION
		 SELECT owner_id FROM teams WHERE id = ?`, teamID, teamID)
	if err != nil {
		return nil, fmt.Errorf("list managers: %w", err)
	}
	defer rows.Close()
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

// TeamIDs lists every workspace, for the periodic checks.
func (d *DB) TeamIDs(ctx context.Context) ([]int64, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT id FROM teams ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list teams: %w", err)
	}
	defer rows.Close()
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

// ClaimOverloadNotice records that a person's overload this week was reported.
// It returns true only for the first claim, so the notice goes out once.
// The claim is stored before the notice is sent: a failed send is then not
// retried, which is the safer side of a nudge that must not repeat.
func (d *DB) ClaimOverloadNotice(ctx context.Context, request appmodel.OverloadNoticeRequest) (bool, error) {
	result, err := d.sql.ExecContext(ctx,
		`INSERT INTO schedule_overload_reminders (team_id, user_id, week, created_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (team_id, user_id, week) DO NOTHING`,
		request.TeamID, request.UserID, request.Week, FormatTime(d.currentTime().UTC()))
	if err != nil {
		return false, fmt.Errorf("claim overload notice: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}
