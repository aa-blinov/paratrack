package db

import (
	"context"
	"database/sql"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// SetDayMark records a member's own mark on a day, or clears it with an empty kind
// (TIME-13). The member must belong to the workspace; the insert checks that itself.
func (d *DB) SetDayMark(ctx context.Context, request appmodel.DayMarkRequest) error {
	if request.TeamID <= 0 || request.UserID <= 0 || request.Day == "" {
		return ErrNotFound
	}
	if request.Kind == "" {
		_, err := d.sql.ExecContext(ctx,
			`DELETE FROM day_marks WHERE team_id = ? AND user_id = ? AND day = ?`,
			request.TeamID, request.UserID, request.Day)
		return err
	}
	res, err := d.sql.ExecContext(ctx,
		`INSERT INTO day_marks (team_id, user_id, day, kind, note, created_at)
		 SELECT ?, ?, ?, ?, ?, ?
		  WHERE EXISTS (SELECT 1 FROM memberships WHERE team_id = ? AND user_id = ?)
		 ON CONFLICT (team_id, user_id, day) DO UPDATE SET kind = excluded.kind, note = excluded.note, created_at = excluded.created_at`,
		request.TeamID, request.UserID, request.Day, request.Kind, request.Note, FormatTime(request.Now),
		request.TeamID, request.UserID)
	if err != nil {
		return err
	}
	if affected, err := res.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return model.ErrForbidden
	}
	return nil
}

// DayMarks lists the marks of a workspace for the days from `from` up to `until`, both
// as YYYY-MM-DD (TIME-13).
func (d *DB) DayMarks(ctx context.Context, teamID int64, from, until string) ([]appmodel.DayMark, error) {
	if teamID <= 0 {
		return nil, ErrNotFound
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT user_id, day, kind, note FROM day_marks
		  WHERE team_id = ? AND day >= ? AND day < ? ORDER BY day, user_id`, teamID, from, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []appmodel.DayMark
	for rows.Next() {
		var mark appmodel.DayMark
		if err := rows.Scan(&mark.UserID, &mark.Day, &mark.Kind, &mark.Note); err != nil {
			return nil, err
		}
		out = append(out, mark)
	}
	return out, rows.Err()
}

// SetTeamHoliday marks a workspace day off (a public holiday nobody is
// expected to work), or clears it.
func (d *DB) SetTeamHoliday(ctx context.Context, request appmodel.TeamHolidayRequest) error {
	teamID, day, name, clear := request.TeamID, request.Day, request.Name, request.Clear
	if teamID <= 0 || day == "" {
		return ErrNotFound
	}
	if clear {
		_, err := d.sql.ExecContext(ctx, `DELETE FROM team_holidays WHERE team_id = ? AND day = ?`, teamID, day)
		return err
	}
	_, err := d.sql.ExecContext(ctx,
		`INSERT INTO team_holidays (team_id, day, name) VALUES (?, ?, ?)
		 ON CONFLICT (team_id, day) DO UPDATE SET name = excluded.name`, teamID, day, name)
	return err
}

// TeamHolidays lists the workspace's days off from `from` up to `until`, both
// YYYY-MM-DD, as day -> name.
func (d *DB) TeamHolidays(ctx context.Context, teamID int64, from, until string) (map[string]string, error) {
	if teamID <= 0 {
		return nil, ErrNotFound
	}
	out := map[string]string{}
	err := queryRows(ctx, d.sql, "team holidays",
		`SELECT day, name FROM team_holidays WHERE team_id = ? AND day >= ? AND day < ? ORDER BY day`,
		func(rows *sql.Rows) error {
			var day, name string
			if err := rows.Scan(&day, &name); err != nil {
				return err
			}
			out[day] = name
			return nil
		}, teamID, from, until)
	return out, err
}
