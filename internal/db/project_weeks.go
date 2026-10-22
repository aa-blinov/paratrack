package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// SetProjectWeeklyNorm stores a project's weekly norm in minutes, or removes it
// when the minutes are zero (PLAN-02). Only a manager sets it.
func (d *DB) SetProjectWeeklyNorm(ctx context.Context, request appmodel.ProjectWeeklyNormRequest) error {
	if request.TeamID <= 0 || request.ProjectID <= 0 || request.CallerID <= 0 || request.Minutes < 0 {
		return ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return err
	}
	if err := requireProjectInTeam(ctx, tx, request.ProjectID, request.TeamID); err != nil {
		return err
	}
	if request.Minutes == 0 {
		_, err = tx.ExecContext(ctx,
			`DELETE FROM project_weekly_norms WHERE team_id = ? AND project_id = ?`, request.TeamID, request.ProjectID)
	} else {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO project_weekly_norms (team_id, project_id, minutes) VALUES (?, ?, ?)
			 ON CONFLICT (project_id) DO UPDATE SET minutes = excluded.minutes`,
			request.TeamID, request.ProjectID, request.Minutes)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// GetProjectWeeklyNorm returns the weekly norm in minutes and whether the project has one.
func (d *DB) GetProjectWeeklyNorm(ctx context.Context, teamID, projectID int64) (int, bool, error) {
	if teamID <= 0 || projectID <= 0 {
		return 0, false, ErrNotFound
	}
	var minutes int
	err := d.sql.QueryRowContext(ctx,
		`SELECT minutes FROM project_weekly_norms WHERE team_id = ? AND project_id = ?`, teamID, projectID).Scan(&minutes)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read project weekly norm: %w", err)
	}
	return minutes, true, nil
}

// ListProjectWeekTotals sums, for each week given by its first day, the plan on
// the project and the time the team tracked on it (PLAN-02). A week is seven days
// from its first day, in the location of that day; open timers count up to now.
func (d *DB) ListProjectWeekTotals(ctx context.Context, teamID, projectID int64, weeks []time.Time, now time.Time) ([]appmodel.ProjectWeekTotal, error) {
	if teamID <= 0 || projectID <= 0 {
		return nil, ErrNotFound
	}
	out := make([]appmodel.ProjectWeekTotal, len(weeks))
	for i, week := range weeks {
		out[i].Start = week
	}
	if len(weeks) == 0 {
		return out, nil
	}
	first, end := weeks[0], weeks[len(weeks)-1].AddDate(0, 0, 7)
	rows, err := d.sql.QueryContext(ctx,
		`SELECT day, COALESCE(SUM(minutes), 0) FROM schedule_entries WHERE team_id = ? AND project_id = ? AND day >= ? AND day < ? GROUP BY day`,
		teamID, projectID, scheduleDay(first), scheduleDay(end))
	if err != nil {
		return nil, fmt.Errorf("sum project plan by day: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var day string
		var minutes int
		if err := rows.Scan(&day, &minutes); err != nil {
			return nil, err
		}
		at, err := time.ParseInLocation("2006-01-02", day, first.Location())
		if err != nil {
			return nil, fmt.Errorf("plan day %q: %w", day, err)
		}
		for i, week := range weeks {
			if !at.Before(week) && at.Before(week.AddDate(0, 0, 7)) {
				out[i].PlannedMinutes += minutes
				break
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sessions, err := d.projectSessions(ctx, teamID, projectID)
	if err != nil {
		return nil, err
	}
	for _, session := range sessions {
		for i, week := range weeks {
			out[i].TrackedSeconds += session.TrackedSecondsInWindow(week, week.AddDate(0, 0, 7), now)
		}
	}
	return out, nil
}

// projectSessions reads the team's sessions on one project, with their owners.
func (d *DB) projectSessions(ctx context.Context, teamID, projectID int64) ([]model.Session, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT s.user_id, s.start_at, s.end_at, s.accumulated_seconds, s.paused, s.last_resume_at
		 FROM sessions s JOIN activities a ON a.id = s.activity_id AND a.team_id = s.team_id
		 WHERE s.team_id = ? AND `+sessionProject+` = ?`,
		teamID, projectID)
	if err != nil {
		return nil, fmt.Errorf("read project sessions: %w", err)
	}
	defer rows.Close()
	var out []model.Session
	for rows.Next() {
		var (
			userID              sql.NullInt64
			startAt             string
			endAt, lastResumeAt sql.NullString
			accumulated, paused int
		)
		if err := rows.Scan(&userID, &startAt, &endAt, &accumulated, &paused, &lastResumeAt); err != nil {
			return nil, err
		}
		if !userID.Valid {
			continue
		}
		session, err := scheduleSession(startAt, endAt, lastResumeAt, accumulated, paused)
		if err != nil {
			return nil, err
		}
		out = append(out, session)
	}
	return out, rows.Err()
}
