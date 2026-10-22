package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// MarkWorkday sets the start of a member's day, once, or its end, after the start
// (TIME-06). The member must belong to the workspace; the insert checks that itself.
func (d *DB) MarkWorkday(ctx context.Context, request appmodel.WorkdayRequest) error {
	if request.TeamID <= 0 || request.UserID <= 0 || request.Day == "" {
		return ErrNotFound
	}
	now := FormatTime(request.Now)
	switch request.Action {
	case "start":
		res, err := d.sql.ExecContext(ctx,
			`INSERT INTO workday_marks (team_id, user_id, day, started_at)
			 SELECT ?, ?, ?, ? WHERE EXISTS (SELECT 1 FROM memberships WHERE team_id = ? AND user_id = ?)
			 ON CONFLICT (team_id, user_id, day) DO NOTHING`,
			request.TeamID, request.UserID, request.Day, now, request.TeamID, request.UserID)
		if err != nil {
			return err
		}
		if affected, err := res.RowsAffected(); err != nil {
			return err
		} else if affected == 0 && !d.hasWorkday(ctx, request) {
			return model.ErrForbidden
		}
		return nil
	case "end":
		res, err := d.sql.ExecContext(ctx,
			`UPDATE workday_marks SET ended_at = ? WHERE team_id = ? AND user_id = ? AND day = ?`,
			now, request.TeamID, request.UserID, request.Day)
		if err != nil {
			return err
		}
		if affected, err := res.RowsAffected(); err != nil {
			return err
		} else if affected == 0 {
			return model.ErrNotFound
		}
		return nil
	default:
		return ErrNotFound
	}
}

// hasWorkday tells whether the day already has its start, for a repeated start click.
func (d *DB) hasWorkday(ctx context.Context, request appmodel.WorkdayRequest) bool {
	var n int
	return d.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workday_marks WHERE team_id = ? AND user_id = ? AND day = ?`,
		request.TeamID, request.UserID, request.Day).Scan(&n) == nil && n > 0
}

// WorkdayOf reads a member's day in the workspace; ok is false when it has no start yet.
func (d *DB) WorkdayOf(ctx context.Context, teamID, userID int64, day string) (appmodel.WorkdayMark, bool, error) {
	if teamID <= 0 || userID <= 0 {
		return appmodel.WorkdayMark{}, false, ErrNotFound
	}
	var startedAt, endedAt string
	err := d.sql.QueryRowContext(ctx,
		`SELECT started_at, ended_at FROM workday_marks WHERE team_id = ? AND user_id = ? AND day = ?`,
		teamID, userID, day).Scan(&startedAt, &endedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return appmodel.WorkdayMark{}, false, nil
	}
	if err != nil {
		return appmodel.WorkdayMark{}, false, err
	}
	mark := appmodel.WorkdayMark{Day: day}
	if mark.StartedAt, err = ScanTime(startedAt); err != nil {
		return appmodel.WorkdayMark{}, false, err
	}
	if endedAt != "" {
		if mark.EndedAt, err = ScanTime(endedAt); err != nil {
			return appmodel.WorkdayMark{}, false, err
		}
	}
	return mark, true, nil
}
