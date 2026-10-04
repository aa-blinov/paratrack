package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// AssignActivityProject sets the project_id on an activity, or clears
// it (projectID == 0). Validates that the project (if non-zero) lives
// in the same team as the activity to prevent cross-team data leak.
func (d *DB) AssignActivityProject(ctx context.Context, request appmodel.AssignActivityProjectRequest) error {
	if request.TeamID <= 0 || request.ActivityID <= 0 || request.ProjectID < 0 || request.CallerID <= 0 {
		return ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return err
	}
	if request.ProjectID > 0 {
		var projectTeamID int64
		err := tx.QueryRowContext(ctx,
			`SELECT team_id FROM projects WHERE id = ? AND team_id = ? FOR UPDATE`, request.ProjectID, request.TeamID).Scan(&projectTeamID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
	}
	var pid any
	if request.ProjectID > 0 {
		pid = request.ProjectID
	}
	now := FormatTime(d.currentTime().UTC())
	res, err := tx.ExecContext(ctx,
		`UPDATE activities SET project_id = ?, updated_at = ?
		 WHERE id = ? AND team_id = ?
		 AND (project_id IS NOT DISTINCT FROM ? OR NOT EXISTS (
		   SELECT 1 FROM sessions WHERE activity_id = activities.id AND invoice_id IS NOT NULL))`,
		pid, now, request.ActivityID, request.TeamID, pid)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		if a, err := scanActivity(tx.QueryRowContext(ctx,
			`SELECT id, name, team_id, project_id, archived, created_at, updated_at FROM activities WHERE id = ?`, request.ActivityID)); err == nil && a.TeamID == request.TeamID {
			return ErrAlreadyBilled
		}
		return ErrNotFound
	}
	return tx.Commit()
}

// AssignFirstActivityProject is the member-safe variant: it only attaches an
// unassigned activity. The old value is part of the UPDATE predicate, so a
// concurrent project assignment cannot be overwritten after policy checking.
func (d *DB) AssignFirstActivityProject(ctx context.Context, request appmodel.AssignActivityProjectRequest) error {
	if request.TeamID <= 0 || request.ActivityID <= 0 || request.ProjectID <= 0 || request.CallerID <= 0 {
		return ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var lockedTeamID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM teams WHERE id = ? FOR UPDATE`, request.TeamID).Scan(&lockedTeamID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	var memberID int64
	if err := tx.QueryRowContext(ctx,
		`SELECT user_id FROM memberships WHERE team_id = ? AND user_id = ? FOR UPDATE`, request.TeamID, request.CallerID).Scan(&memberID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ErrForbidden
		}
		return err
	}
	var projectTeamID int64
	if err := tx.QueryRowContext(ctx,
		`SELECT team_id FROM projects WHERE id = ? AND team_id = ? FOR UPDATE`, request.ProjectID, request.TeamID).Scan(&projectTeamID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE activities SET project_id = ?, updated_at = ?
		 WHERE id = ? AND team_id = ? AND COALESCE(project_id, 0) = 0
		 AND NOT EXISTS (SELECT 1 FROM sessions WHERE activity_id = activities.id AND invoice_id IS NOT NULL)`,
		request.ProjectID, FormatTime(d.currentTime().UTC()), request.ActivityID, request.TeamID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n > 0 {
		return tx.Commit()
	}
	activity, err := scanActivity(tx.QueryRowContext(ctx,
		`SELECT id, name, team_id, project_id, archived, created_at, updated_at FROM activities WHERE id = ?`, request.ActivityID))
	if errors.Is(err, ErrNotFound) || (err == nil && activity.TeamID != request.TeamID) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if activity.ProjectID != 0 {
		return model.ErrForbidden
	}
	return ErrAlreadyBilled
}

func (d *DB) UnassignedActivities(ctx context.Context, teamID int64) ([]appmodel.UnassignedActivity, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT a.id, a.name, COUNT(s.id) FILTER (WHERE s.invoice_id IS NULL),
		       COUNT(s.id) FILTER (WHERE s.invoice_id IS NOT NULL)
		FROM activities a JOIN sessions s ON s.activity_id = a.id
		WHERE a.team_id = ? AND s.team_id = a.team_id AND a.project_id IS NULL AND s.end_at IS NOT NULL
		GROUP BY a.id, a.name
		HAVING COUNT(s.id) FILTER (WHERE s.invoice_id IS NULL) > 0
		ORDER BY a.name`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []appmodel.UnassignedActivity
	for rows.Next() {
		var a appmodel.UnassignedActivity
		var billed int64
		if err := rows.Scan(&a.ID, &a.Name, &a.Sessions, &billed); err != nil {
			return nil, err
		}
		a.Billed = billed > 0
		out = append(out, a)
	}
	return out, rows.Err()
}

// AssignUnassignedActivityForBilling deliberately changes the activity,
// not just one session: all its past and future time will follow the project.
// Do not move an activity that has already been included in any invoice.
func (d *DB) AssignUnassignedActivityForBilling(ctx context.Context, request appmodel.AssignActivityProjectRequest) error {
	teamID, activityID, projectID, callerID := request.TeamID, request.ActivityID, request.ProjectID, request.CallerID
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return err
	}
	p, err := scanProject(tx.QueryRowContext(ctx,
		`SELECT id, team_id, slug, name, color, archived, estimate_minutes, billable_rate_cents, billable, created_at, updated_at
		 FROM projects WHERE id = ? AND team_id = ? FOR UPDATE`, projectID, teamID))
	if err != nil {
		return err
	}
	if p.Archived || !p.Billable || p.BillableRateCents == nil || *p.BillableRateCents <= 0 {
		return ErrNotFound
	}
	var lockedID int64
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM activities WHERE id = ? AND team_id = ? AND project_id IS NULL FOR UPDATE`, activityID, teamID).Scan(&lockedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE activities SET project_id = ?, updated_at = ?
		WHERE id = ? AND team_id = ? AND project_id IS NULL
		AND NOT EXISTS (SELECT 1 FROM sessions WHERE activity_id = activities.id AND invoice_id IS NOT NULL)`,
		projectID, FormatTime(d.currentTime().UTC()), activityID, teamID)
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
