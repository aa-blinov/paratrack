package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

// ErrGoalNotFound is returned when a goal is missing.
var ErrGoalNotFound = errors.New("goal not found")

// UpsertGoal inserts or replaces the goal for (activity_id, period).
// One goal per (activity, period) pair — setting "reading daily 2h"
// when one already exists just updates the target_minutes.
func (d *DB) UpsertGoal(ctx context.Context, activityID int64, period string, targetMinutes int) (model.Goal, error) {
	if targetMinutes <= 0 {
		return model.Goal{}, fmt.Errorf("target_minutes must be positive, got %d", targetMinutes)
	}
	switch period {
	case "daily", "weekly", "monthly":
	default:
		return model.Goal{}, fmt.Errorf("period must be daily|weekly|monthly, got %q", period)
	}
	now := FormatTime(time.Now().UTC())
	_, err := d.sql.ExecContext(ctx, `
		INSERT INTO goals (activity_id, period, target_minutes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(activity_id, period) DO UPDATE SET
			target_minutes = excluded.target_minutes,
			updated_at = excluded.updated_at
	`, activityID, period, targetMinutes, now, now)
	if err != nil {
		return model.Goal{}, err
	}
	// Read back the row — id is auto-assigned on first insert, kept on update.
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, activity_id, period, target_minutes, created_at, updated_at
		 FROM goals WHERE activity_id = ? AND period = ?`,
		activityID, period)
	return scanGoal(row)
}

// ListGoals returns every configured goal. If activityID is non-nil the
// list is filtered to that single activity.
func (d *DB) ListGoals(ctx context.Context, activityID *int64) ([]model.Goal, error) {
	q := `SELECT id, activity_id, period, target_minutes, created_at, updated_at FROM goals`
	args := []any{}
	if activityID != nil {
		q += ` WHERE activity_id = ?`
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

// DeleteGoal removes the goal for (activity_id, period). Returns
// ErrGoalNotFound if no such row existed.
func (d *DB) DeleteGoal(ctx context.Context, activityID int64, period string) error {
	res, err := d.sql.ExecContext(ctx,
		`DELETE FROM goals WHERE activity_id = ? AND period = ?`,
		activityID, period)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrGoalNotFound
	}
	return nil
}

// GoalProgress is the actual time accumulated against a goal in its period.
type GoalProgress struct {
	Goal            model.Goal `json:"goal"`
	ActivityName       string      `json:"activity_name"`
	AchievedMinutes int         `json:"achieved_minutes"` // minutes so far in the period
	PercentComplete int         `json:"percent_complete"` // 0..100+ (capped at 100 for display elsewhere)
	PeriodStart     time.Time   `json:"period_start"`
	PeriodEnd       time.Time   `json:"period_end"`
}

// ProgressForGoals joins the configured goals with the actual minutes
// tracked in each goal's current period. Active sessions contribute
// accumulated_seconds + (now - last_resume_at) at call-time.
//
// period times are computed against `now` so the caller can pin time
// for tests by passing a fixed value.
func (d *DB) ProgressForGoals(ctx context.Context, now time.Time) ([]GoalProgress, error) {
	goals, err := d.ListGoals(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := make([]GoalProgress, 0, len(goals))
	for _, g := range goals {
		start, end := goalPeriodRange(g.Period, now)
		minutes, err := d.activityMinutesInRange(ctx, g.ActivityID, start, end, now)
		if err != nil {
			return nil, err
		}
		// Look up the activity name for display.
		var name string
		if err := d.sql.QueryRowContext(ctx,
			`SELECT name FROM activities WHERE id = ?`, g.ActivityID,
		).Scan(&name); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		pct := 0
		if g.TargetMinutes > 0 {
			pct = minutes * 100 / g.TargetMinutes
		}
		out = append(out, GoalProgress{
			Goal:            g,
			ActivityName:      name,
			AchievedMinutes: minutes,
			PercentComplete: pct,
			PeriodStart:      start,
			PeriodEnd:        end,
		})
	}
	return out, nil
}

// activityMinutesInRange sums session minutes overlapping [start,end],
// clipped to the window, and includes the live elapsed time of any
// active session as of `now`.
func (d *DB) activityMinutesInRange(ctx context.Context, activityID int64, start, end, now time.Time) (int, error) {
	// Closed sessions: sum seconds, clipped per row.
	rows, err := d.sql.QueryContext(ctx, `
		SELECT start_at, end_at, accumulated_seconds, paused, last_resume_at
		FROM sessions
		WHERE activity_id = ?
		  AND (
		    (end_at IS NOT NULL AND start_at <= ? AND end_at >= ?)
		    OR (end_at IS NULL AND start_at <= ?)
		  )
	`, activityID, FormatTime(end), FormatTime(start), FormatTime(end))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var totalSec int
	for rows.Next() {
		var (
			sStart, sEnd       sql.NullString
			accumulated        int
			paused             int
			lastResume         sql.NullString
		)
		if err := rows.Scan(&sStart, &sEnd, &accumulated, &paused, &lastResume); err != nil {
			return 0, err
		}
		st, err := ScanTime(sStart.String)
		if err != nil {
			return 0, err
		}
		// Clip start to window.
		if st.Before(start) {
			st = start
		}
		var et time.Time
		if sEnd.Valid {
			t, err := ScanTime(sEnd.String)
			if err != nil {
				return 0, err
			}
			et = t
		} else {
			// Active session: live as of `now`, capped at `end`.
			liveNow := now
			if liveNow.After(end) {
				liveNow = end
			}
			// Plus accumulated + (now - last_resume_at) if not paused.
			base := accumulated
			if paused == 0 && lastResume.Valid {
				lr, _ := ScanTime(lastResume.String)
				if lr.Before(st) {
					lr = st
				}
				if lr.After(liveNow) {
					// clock skew — ignore the live portion
				} else {
					base += int(liveNow.Sub(lr).Seconds())
				}
			}
			totalSec += base
			continue
		}
		// Clip end to window.
		if et.After(end) {
			et = end
		}
		if et.Before(st) {
			continue
		}
		totalSec += int(et.Sub(st).Seconds())
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return totalSec / 60, nil
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
		// ISO week starts Monday.
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7 // Sunday → 7
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
	// Defensive fallback — should be impossible after UpsertGoal validation.
	y, mo, d := now.Date()
	return time.Date(y, mo, d, 0, 0, 0, 0, time.UTC),
		time.Date(y, mo, d, 0, 0, 0, 0, time.UTC).Add(24 * time.Hour)
}

func scanGoal(r row) (model.Goal, error) {
	var (
		g         model.Goal
		createdAt string
		updatedAt string
	)
	if err := r.Scan(&g.ID, &g.ActivityID, &g.Period, &g.TargetMinutes, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Goal{}, ErrGoalNotFound
		}
		return model.Goal{}, err
	}
	if t, err := ScanTime(createdAt); err == nil {
		g.CreatedAt = t
	}
	if t, err := ScanTime(updatedAt); err == nil {
		g.UpdatedAt = t
	}
	return g, nil
}