package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// sessionProject is the project a session counts under: its own project when it
// has one, its activity's project otherwise. A query that uses it names the
// sessions table s and the activities table a, and joins projects on this value,
// so reports, invoices and payouts all count a session under the same project.
const sessionProject = "COALESCE(s.project_id, a.project_id)"

// sessionEditProject is the value a project correction writes to one session. A
// session that is part of an invoice keeps its project, whatever the invoice's
// status: its lines were priced with it. Zero returns the session to its activity's
// project, and a project must belong to the workspace.
func sessionEditProject(ctx context.Context, tx *Tx, teamID int64, state sessionEditState, projectID int64) (any, error) {
	if state.invoiceID != 0 {
		return nil, model.ErrAlreadyBilled
	}
	if projectID <= 0 {
		return nil, nil
	}
	var projectTeam int64
	if err := tx.QueryRowContext(ctx,
		`SELECT team_id FROM projects WHERE id = ? AND team_id = ?`, projectID, teamID).Scan(&projectTeam); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	// The activity's own project is not stored on the session: the session keeps
	// following the activity if that project changes later.
	var activityProject sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT project_id FROM activities WHERE id = ?`, state.activityID).Scan(&activityProject); err != nil {
		return nil, err
	}
	if activityProject.Valid && activityProject.Int64 == projectID {
		return nil, nil
	}
	return projectID, nil
}

// sessionOwnProject decides what sessions.project_id a session takes when it is
// started or added now. nil means the session takes its activity's project. An
// activity with no project yet takes the project being started with, as the first
// project it gets: its unbilled time then counts under it. This is the only way a
// start assigns the activity's project, and only while no session of the activity
// is on an invoice. Any other project that differs from the activity's is kept on
// the session alone.
func sessionOwnProject(ctx context.Context, tx *Tx, teamID, activityID, projectID int64, now string) (any, error) {
	if projectID <= 0 {
		return nil, nil
	}
	var projectTeam int64
	if err := tx.QueryRowContext(ctx,
		`SELECT team_id FROM projects WHERE id = ? AND team_id = ?`, projectID, teamID).Scan(&projectTeam); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var activityProject sql.NullInt64
	var invoiced bool
	if err := tx.QueryRowContext(ctx,
		`SELECT a.project_id, EXISTS (SELECT 1 FROM sessions WHERE activity_id = a.id AND invoice_id IS NOT NULL)
		 FROM activities a WHERE a.id = ? AND a.team_id = ?`, activityID, teamID).Scan(&activityProject, &invoiced); err != nil {
		return nil, err
	}
	if activityProject.Valid && activityProject.Int64 == projectID {
		return nil, nil
	}
	if !activityProject.Valid && !invoiced {
		if _, err := tx.ExecContext(ctx,
			`UPDATE activities SET project_id = ?, updated_at = ?
			 WHERE id = ? AND project_id IS NULL
			   AND NOT EXISTS (SELECT 1 FROM sessions WHERE activity_id = activities.id AND invoice_id IS NOT NULL)`,
			projectID, now, activityID); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return projectID, nil
}

// SessionProjectImpact reads what moving a session to a project would move, under
// the rules sessionOwnProject applies when the change is written.
func (d *DB) SessionProjectImpact(ctx context.Context, query appmodel.SessionProjectImpactQuery) (appmodel.SessionProjectImpact, error) {
	var activityID int64
	if err := d.sql.QueryRowContext(ctx,
		`SELECT activity_id FROM sessions WHERE id = ? AND team_id = ?`, query.SessionID, query.TeamID).Scan(&activityID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return appmodel.SessionProjectImpact{}, ErrNotFound
		}
		return appmodel.SessionProjectImpact{}, err
	}
	var activityProject sql.NullInt64
	var billed bool
	if err := d.sql.QueryRowContext(ctx,
		`SELECT a.project_id, EXISTS (SELECT 1 FROM sessions WHERE activity_id = a.id AND invoice_id IS NOT NULL)
		   FROM activities a WHERE a.id = ? AND a.team_id = ?`, activityID, query.TeamID).Scan(&activityProject, &billed); err != nil {
		return appmodel.SessionProjectImpact{}, err
	}
	if query.ProjectID <= 0 || activityProject.Valid || billed {
		return appmodel.SessionProjectImpact{Sessions: 1}, nil
	}
	var impact appmodel.SessionProjectImpact
	impact.Binds = true
	if err := d.sql.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(accumulated_seconds), 0) FROM sessions
		  WHERE activity_id = ? AND team_id = ? AND project_id IS NULL`,
		activityID, query.TeamID).Scan(&impact.Sessions, &impact.Seconds); err != nil {
		return appmodel.SessionProjectImpact{}, err
	}
	return impact, nil
}
