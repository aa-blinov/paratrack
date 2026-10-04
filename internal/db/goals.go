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

// UpsertGoalForManager atomically creates/resolves the activity and writes its
// goal after rechecking the caller's manager role under the workspace lock.
func (d *DB) UpsertGoalForManager(ctx context.Context, request appmodel.GoalUpsertRequest) (model.Goal, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return model.Goal{}, model.ErrForbidden
	}
	if request.Minutes <= 0 {
		return model.Goal{}, fmt.Errorf("target_minutes must be positive, got %d", request.Minutes)
	}
	switch request.Period {
	case "daily", "weekly", "monthly":
	default:
		return model.Goal{}, fmt.Errorf("period must be daily|weekly|monthly, got %q", request.Period)
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.Goal{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return model.Goal{}, err
	}
	name := strings.TrimSpace(request.ActivityName)
	now := FormatTime(d.currentTime().UTC())
	if name == "" {
		return model.Goal{}, fmt.Errorf("activity name cannot be empty")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO activities (name, name_key, team_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, name, strings.ToLower(name), request.TeamID, now, now); err != nil {
		return model.Goal{}, err
	}
	var activityID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM activities WHERE name_key = ? AND team_id = ?`, strings.ToLower(name), request.TeamID).Scan(&activityID); err != nil {
		return model.Goal{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO goals (activity_id, team_id, period, target_minutes, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(team_id, activity_id, period) DO UPDATE SET target_minutes = excluded.target_minutes, updated_at = excluded.updated_at`, activityID, request.TeamID, request.Period, request.Minutes, now, now); err != nil {
		return model.Goal{}, err
	}
	goal, err := scanGoal(tx.QueryRowContext(ctx, `SELECT id, activity_id, team_id, period, target_minutes, created_at, updated_at FROM goals WHERE team_id = ? AND activity_id = ? AND period = ?`, request.TeamID, activityID, request.Period))
	if err != nil {
		return model.Goal{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Goal{}, err
	}
	return goal, nil
}

// DeleteGoalForManager removes a goal after a transactional role recheck.
func (d *DB) DeleteGoalForManager(ctx context.Context, request appmodel.GoalDeleteRequest) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return err
	}
	var activityID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM activities WHERE name_key = ? AND team_id = ?`, strings.ToLower(strings.TrimSpace(request.ActivityName)), request.TeamID).Scan(&activityID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ErrNotFound
		}
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM goals WHERE team_id = ? AND activity_id = ? AND period = ?`, request.TeamID, activityID, request.Period)
	if err != nil {
		return err
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if deleted == 0 {
		return model.ErrGoalNotFound
	}
	return tx.Commit()
}

// ListGoals returns every configured goal in the given team. If
// activityID is non-nil the list is filtered to that single activity.
func (d *DB) ListGoals(ctx context.Context, teamID int64, activityID *int64) ([]model.Goal, error) {
	if teamID <= 0 {
		return nil, ErrNotFound
	}
	q := `SELECT id, activity_id, team_id, period, target_minutes, created_at, updated_at FROM goals WHERE team_id = ?`
	args := []any{teamID}
	if activityID != nil {
		q += ` AND activity_id = ?`
		args = append(args, *activityID)
	}
	q += ` ORDER BY activity_id, period`
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Goal
	for rows.Next() {
		g, err := scanGoal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GoalProgress is kept as an adapter alias for callers that used the older
// db package type name.
type GoalProgress = model.GoalProgress

// ProgressForGoals joins the configured goals (in teamID) with the
// actual minutes tracked in each goal's current period. Active
// sessions contribute accumulated_seconds + (now - last_resume_at) at
// call-time.
//
// period times are computed against `now` so the caller can pin time
// for tests by passing a fixed value.
func (d *DB) ProgressForGoals(ctx context.Context, teamID int64, now time.Time) ([]GoalProgress, error) {
	goals, err := d.ListGoals(ctx, teamID, nil)
	if err != nil {
		return nil, err
	}
	if len(goals) == 0 {
		return []GoalProgress{}, nil
	}

	activityIDs := make([]int64, 0, len(goals))
	seenActivities := make(map[int64]struct{}, len(goals))
	windows := make(map[int64][2]time.Time, len(goals))
	var rangeStart, rangeEnd time.Time
	for _, goal := range goals {
		start, end := goalPeriodRange(goal.Period, now)
		windows[goal.ID] = [2]time.Time{start, end}
		if rangeStart.IsZero() || start.Before(rangeStart) {
			rangeStart = start
		}
		if end.After(rangeEnd) {
			rangeEnd = end
		}
		if _, exists := seenActivities[goal.ActivityID]; !exists {
			seenActivities[goal.ActivityID] = struct{}{}
			activityIDs = append(activityIDs, goal.ActivityID)
		}
	}

	activityNames := make(map[int64]string, len(activityIDs))
	activityRows, err := d.sql.QueryContext(ctx,
		`SELECT id, name FROM activities WHERE team_id = ? AND id = ANY(?)`, teamID, activityIDs)
	if err != nil {
		return nil, err
	}
	defer activityRows.Close()
	for activityRows.Next() {
		var id int64
		var name string
		if err := activityRows.Scan(&id, &name); err != nil {
			_ = activityRows.Close()
			return nil, err
		}
		activityNames[id] = name
	}
	if err := activityRows.Err(); err != nil {
		_ = activityRows.Close()
		return nil, err
	}
	if err := activityRows.Close(); err != nil {
		return nil, err
	}

	sessionsByActivity, err := d.goalSessionsForActivities(ctx, teamID, activityIDs, rangeStart, rangeEnd)
	if err != nil {
		return nil, err
	}
	out := make([]GoalProgress, 0, len(goals))
	for _, goal := range goals {
		window := windows[goal.ID]
		var totalSeconds int
		for _, session := range sessionsByActivity[goal.ActivityID] {
			seconds := session.TrackedSecondsInWindow(window[0], window[1], now)
			totalSeconds, err = money.AddInt(totalSeconds, seconds)
			if err != nil {
				return nil, fmt.Errorf("sum tracked time for goal %d: %w", goal.ID, err)
			}
		}
		minutes := totalSeconds / 60
		pct := money.PercentRatio(minutes, goal.TargetMinutes, 100, 1)
		out = append(out, GoalProgress{
			Goal:            goal,
			ActivityName:    activityNames[goal.ActivityID],
			AchievedMinutes: minutes,
			PercentComplete: pct,
			PeriodStart:     window[0],
			PeriodEnd:       window[1],
		})
	}
	return out, nil
}

func (d *DB) goalSessionsForActivities(ctx context.Context, teamID int64, activityIDs []int64, start, end time.Time) (map[int64][]model.Session, error) {
	q := `SELECT s.activity_id, s.start_at, s.end_at, s.accumulated_seconds, s.paused, s.last_resume_at
	      FROM sessions s
	      WHERE s.team_id = ? AND s.activity_id = ANY(?)
	        AND ((s.end_at IS NOT NULL AND s.start_at <= ? AND s.end_at >= ?)
	          OR (s.end_at IS NULL AND s.start_at <= ?))`
	args := []any{teamID, activityIDs, FormatTime(end), FormatTime(start), FormatTime(end)}
	var scope string
	scope, args = scopeSQL(ctx, "s.user_id", args)
	rows, err := d.sql.QueryContext(ctx, q+scope, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sessions := make(map[int64][]model.Session)
	for rows.Next() {
		var (
			activityID   int64
			startAt      string
			endAt        sql.NullString
			accumulated  int
			paused       int
			lastResumeAt sql.NullString
		)
		if err := rows.Scan(&activityID, &startAt, &endAt, &accumulated, &paused, &lastResumeAt); err != nil {
			return nil, err
		}
		started, err := ScanTime(startAt)
		if err != nil {
			return nil, fmt.Errorf("parse goal session start time: %w", err)
		}
		session := model.Session{StartAt: started, AccumulatedSeconds: accumulated, Paused: paused == 1}
		if endAt.Valid {
			ended, err := ScanTime(endAt.String)
			if err != nil {
				return nil, fmt.Errorf("parse goal session end time: %w", err)
			}
			session.EndAt = &ended
		}
		if lastResumeAt.Valid {
			resumed, err := ScanTime(lastResumeAt.String)
			if err != nil {
				return nil, fmt.Errorf("parse goal session resume time: %w", err)
			}
			session.LastResumeAt = &resumed
		}
		sessions[activityID] = append(sessions[activityID], session)
	}
	return sessions, rows.Err()
}

// goalPeriodRange returns the [start, end) window for a goal's period
// containing `now`. end is exclusive to make overlap math easier.
func goalPeriodRange(period string, now time.Time) (time.Time, time.Time) {
	now = now.UTC()
	switch period {
	case "daily":
		y, mo, d := now.Date()
		start := time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)
		end := start.Add(24 * time.Hour)
		return start, end
	case "weekly":
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		monday := now.AddDate(0, 0, -(weekday - 1))
		y, mo, d := monday.Date()
		start := time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)
		end := start.AddDate(0, 0, 7)
		return start, end
	case "monthly":
		y, mo, _ := now.Date()
		start := time.Date(y, mo, 1, 0, 0, 0, 0, time.UTC)
		end := start.AddDate(0, 1, 0)
		return start, end
	}
	y, mo, d := now.Date()
	return time.Date(y, mo, d, 0, 0, 0, 0, time.UTC),
		time.Date(y, mo, d, 0, 0, 0, 0, time.UTC).Add(24 * time.Hour)
}

func scanGoal(r row) (model.Goal, error) {
	var (
		g         model.Goal
		teamID    sql.NullInt64
		createdAt string
		updatedAt string
	)
	if err := r.Scan(&g.ID, &g.ActivityID, &teamID, &g.Period, &g.TargetMinutes, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Goal{}, model.ErrGoalNotFound
		}
		return model.Goal{}, err
	}
	if teamID.Valid {
		g.TeamID = teamID.Int64
	}
	created, err := ScanTime(createdAt)
	if err != nil {
		return model.Goal{}, fmt.Errorf("parse goal creation time: %w", err)
	}
	g.CreatedAt = created
	g.UpdatedAt, err = ScanTime(updatedAt)
	if err != nil {
		return model.Goal{}, fmt.Errorf("parse goal update time: %w", err)
	}
	return g, nil
}
