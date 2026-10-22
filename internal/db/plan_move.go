package db

import (
	"context"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// PlannedFromDayForUser sums what one person still has planned on a project from a
// day onward (TASK-03): what a reassignment would carry to the new assignee.
func (d *DB) PlannedFromDayForUser(ctx context.Context, teamID, projectID, userID int64, from time.Time) (int, error) {
	if teamID <= 0 || projectID <= 0 || userID <= 0 {
		return 0, ErrNotFound
	}
	var minutes int
	if err := d.sql.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(minutes), 0) FROM schedule_entries WHERE team_id = ? AND project_id = ? AND user_id = ? AND day >= ?`,
		teamID, projectID, userID, scheduleDay(from)).Scan(&minutes); err != nil {
		return 0, err
	}
	return minutes, nil
}

// MovePlanFromDay moves what one person has planned on a project from a day onward
// to another person, day by day, and returns the minutes moved (TASK-03). The
// caller must be able to plan the project, and the move is one transaction.
func (d *DB) MovePlanFromDay(ctx context.Context, request appmodel.PlanMoveRequest) (int, error) {
	if request.TeamID <= 0 || request.ProjectID <= 0 || request.FromUserID <= 0 || request.ToUserID <= 0 || request.CallerID <= 0 || request.FromUserID == request.ToUserID {
		return 0, ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := authorizeSchedulePlan(ctx, tx, request.TeamID, request.CallerID, request.ProjectID); err != nil {
		return 0, err
	}
	type dayPlan struct {
		day     string
		minutes int
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT day, minutes FROM schedule_entries WHERE team_id = ? AND project_id = ? AND user_id = ? AND day >= ? AND minutes > 0 ORDER BY day`,
		request.TeamID, request.ProjectID, request.FromUserID, scheduleDay(request.From))
	if err != nil {
		return 0, err
	}
	var plan []dayPlan
	for rows.Next() {
		var p dayPlan
		if err := rows.Scan(&p.day, &p.minutes); err != nil {
			_ = rows.Close()
			return 0, err
		}
		plan = append(plan, p)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	now := FormatTime(d.currentTime().UTC())
	moved := 0
	for _, p := range plan {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schedule_entries (team_id, user_id, project_id, day, minutes, note, created_at) VALUES (?, ?, ?, ?, ?, '', ?)
			 ON CONFLICT (team_id, user_id, project_id, day) DO UPDATE SET minutes = schedule_entries.minutes + excluded.minutes`,
			request.TeamID, request.ToUserID, request.ProjectID, p.day, p.minutes, now); err != nil {
			return 0, err
		}
		moved += p.minutes
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM schedule_entries WHERE team_id = ? AND project_id = ? AND user_id = ? AND day >= ?`,
		request.TeamID, request.ProjectID, request.FromUserID, scheduleDay(request.From)); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return moved, nil
}
