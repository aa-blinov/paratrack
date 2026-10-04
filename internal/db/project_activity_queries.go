package db

import (
	"context"
	"fmt"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// ListActivitiesForProject returns the non-archived activities under
// a project, sorted by name. Used by /projects/{slug} to show what's
// in the project.
func (d *DB) ListActivitiesForProject(ctx context.Context, query appmodel.ProjectActivityCatalogQuery) ([]model.Activity, error) {
	if query.TeamID <= 0 || query.ProjectID <= 0 {
		return nil, ErrNotFound
	}
	q := `SELECT a.id, a.name, a.team_id, a.project_id, a.archived, a.created_at, a.updated_at
	        FROM activities a JOIN projects p ON p.id = a.project_id
	        WHERE a.project_id = ? AND a.team_id = ? AND p.team_id = ?`
	args := []any{query.ProjectID, query.TeamID, query.TeamID}
	if !query.IncludeArchived {
		q += ` AND a.archived = 0`
	}
	q += ` ORDER BY a.name`
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Activity
	for rows.Next() {
		a, err := scanActivity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ProjectSpans returns the closed sessions touching [from, to] that
// belong to a project, in one query for the whole workspace (scoped to
// the person for a member).
func (d *DB) ProjectSpans(ctx context.Context, teamID int64, from, to time.Time) ([]model.ProjectSessionSpan, error) {
	if teamID <= 0 || from.IsZero() || to.Before(from) {
		return nil, ErrNotFound
	}
	q := `SELECT a.project_id, s.start_at, s.end_at, s.accumulated_seconds, s.paused
		FROM sessions s
		JOIN activities a ON a.id = s.activity_id AND a.team_id = s.team_id
		JOIN projects p ON p.id = a.project_id AND p.team_id = s.team_id
		WHERE s.team_id = ? AND a.project_id IS NOT NULL AND s.end_at IS NOT NULL
		  AND s.start_at <= ? AND s.end_at >= ?`
	args := []any{teamID, FormatTime(to), FormatTime(from)}
	sc, args := scopeSQL(ctx, "s.user_id", args)
	rows, err := d.sql.QueryContext(ctx, q+sc, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ProjectSessionSpan
	for rows.Next() {
		var sp model.ProjectSessionSpan
		var st, en string
		var paused int
		if err := rows.Scan(&sp.ProjectID, &st, &en, &sp.Session.AccumulatedSeconds, &paused); err != nil {
			return nil, err
		}
		sp.Session.StartAt, err = ScanTime(st)
		if err != nil {
			return nil, fmt.Errorf("parse project session start: %w", err)
		}
		end, err := ScanTime(en)
		if err != nil {
			return nil, fmt.Errorf("parse project session end: %w", err)
		}
		sp.Session.EndAt = &end
		sp.Session.Paused = paused == 1
		out = append(out, sp)
	}
	return out, rows.Err()
}

// ProjectActivityCounts is the number of activities (archived too) per project.
func (d *DB) ProjectActivityCounts(ctx context.Context, teamID int64) (map[int64]int, error) {
	if teamID <= 0 {
		return nil, ErrNotFound
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT a.project_id, count(*) FROM activities a
		 JOIN projects p ON p.id = a.project_id AND p.team_id = a.team_id
		 WHERE a.team_id = ? AND a.project_id IS NOT NULL GROUP BY a.project_id`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// ProjectSessions is the project's closed sessions touching [from, to],
// newest first (scoped to the person for a member).
func (d *DB) ProjectSessions(ctx context.Context, query appmodel.ProjectActivityQuery) ([]model.ActiveSession, error) {
	if query.TeamID <= 0 || query.ProjectID <= 0 || query.From.IsZero() || query.Through.Before(query.From) {
		return nil, ErrNotFound
	}
	q := sessionSelect + `
	JOIN projects p ON p.id = a.project_id AND p.team_id = s.team_id
		WHERE s.team_id = ? AND a.team_id = s.team_id AND p.team_id = s.team_id AND a.project_id = ?
		  AND s.end_at IS NOT NULL AND s.start_at <= ? AND s.end_at >= ?`
	args := []any{query.TeamID, query.ProjectID, FormatTime(query.Through), FormatTime(query.From)}
	sc, args := scopeSQL(ctx, "s.user_id", args)
	rows, err := d.sql.QueryContext(ctx, q+sc+` ORDER BY s.start_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanActiveSessions(rows)
}

// ProjectTrackedTotal is all the tracked time ever put on the project
// (closed sessions; scoped to the person for a member).
func (d *DB) ProjectTrackedTotal(ctx context.Context, query appmodel.ProjectScopeQuery) (int, error) {
	if query.TeamID <= 0 || query.ProjectID <= 0 {
		return 0, ErrNotFound
	}
	q := `SELECT COALESCE(SUM(CASE WHEN s.accumulated_seconds > 0 THEN s.accumulated_seconds
		       ELSE GREATEST(0, FLOOR(EXTRACT(EPOCH FROM (s.end_at::timestamp - s.start_at::timestamp)))) END), 0)::bigint
		FROM sessions s
		JOIN activities a ON a.id = s.activity_id AND a.team_id = s.team_id
		JOIN projects p ON p.id = a.project_id AND p.team_id = s.team_id
		WHERE s.team_id = ? AND a.project_id = ? AND s.end_at IS NOT NULL`
	args := []any{query.TeamID, query.ProjectID}
	sc, args := scopeSQL(ctx, "s.user_id", args)
	var total int64
	err := d.sql.QueryRowContext(ctx, q+sc, args...).Scan(&total)
	return int(total), err
}
