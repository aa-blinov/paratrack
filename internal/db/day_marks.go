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

// SetTeamHolidays marks workspace days off (public holidays nobody is
// expected to work), or clears them, all days in one transaction.
func (d *DB) SetTeamHolidays(ctx context.Context, request appmodel.TeamHolidayRequest) error {
	if request.TeamID <= 0 || len(request.Days) == 0 {
		return ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, day := range request.Days {
		if request.Clear {
			_, err = tx.ExecContext(ctx, `DELETE FROM team_holidays WHERE team_id = ? AND day = ?`, request.TeamID, day)
		} else {
			kind := "off"
			if request.Work {
				kind = "work"
			}
			name := request.Name
			if named, ok := request.Names[day]; ok {
				name = named
			}
			q := `INSERT INTO team_holidays (team_id, day, name, kind) VALUES (?, ?, ?, ?)
				 ON CONFLICT (team_id, day) DO UPDATE SET name = excluded.name, kind = excluded.kind`
			if request.Keep {
				q = `INSERT INTO team_holidays (team_id, day, name, kind) VALUES (?, ?, ?, ?)
				 ON CONFLICT (team_id, day) DO NOTHING`
			}
			_, err = tx.ExecContext(ctx, q, request.TeamID, day, name, kind)
		}
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetAbsence records a member's absence over days, or clears it, all days in
// one transaction. The member must belong to the workspace.
func (d *DB) SetAbsence(ctx context.Context, request appmodel.AbsenceRequest) error {
	if request.TeamID <= 0 || request.UserID <= 0 || len(request.Days) == 0 {
		return ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var member bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM memberships WHERE team_id = ? AND user_id = ?)`, request.TeamID, request.UserID).Scan(&member); err != nil {
		return err
	}
	if !member {
		return model.ErrForbidden
	}
	for _, day := range request.Days {
		if request.Kind == "" {
			_, err = tx.ExecContext(ctx, `DELETE FROM member_absences WHERE team_id = ? AND user_id = ? AND day = ?`, request.TeamID, request.UserID, day)
		} else {
			_, err = tx.ExecContext(ctx,
				`INSERT INTO member_absences (team_id, user_id, day, kind, note, created_at) VALUES (?, ?, ?, ?, ?, ?)
				 ON CONFLICT (team_id, user_id, day) DO UPDATE SET kind = excluded.kind, note = excluded.note, created_at = excluded.created_at`,
				request.TeamID, request.UserID, day, request.Kind, request.Note, FormatTime(request.Now))
		}
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Absences lists the workspace's absences from `from` up to `until`, both
// YYYY-MM-DD.
func (d *DB) Absences(ctx context.Context, teamID int64, from, until string) ([]appmodel.Absence, error) {
	if teamID <= 0 {
		return nil, ErrNotFound
	}
	var out []appmodel.Absence
	err := queryRows(ctx, d.sql, "absences",
		`SELECT user_id, day, kind, note FROM member_absences WHERE team_id = ? AND day >= ? AND day < ? ORDER BY day, user_id`,
		func(rows *sql.Rows) error {
			var a appmodel.Absence
			if err := rows.Scan(&a.UserID, &a.Day, &a.Kind, &a.Note); err != nil {
				return err
			}
			out = append(out, a)
			return nil
		}, teamID, from, until)
	return out, err
}

// TeamHolidays lists the workspace's days off from `from` up to `until`, both
// YYYY-MM-DD, as day -> name.
func (d *DB) TeamHolidays(ctx context.Context, teamID int64, from, until string) (map[string]string, error) {
	if teamID <= 0 {
		return nil, ErrNotFound
	}
	out := map[string]string{}
	err := queryRows(ctx, d.sql, "team holidays",
		`SELECT day, name FROM team_holidays WHERE team_id = ? AND day >= ? AND day < ? AND kind = 'off' ORDER BY day`,
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

// TeamWorkdays lists the weekend days the workspace works (transfers) from
// `from` up to `until`, as day -> name.
func (d *DB) TeamWorkdays(ctx context.Context, teamID int64, from, until string) (map[string]string, error) {
	if teamID <= 0 {
		return nil, ErrNotFound
	}
	out := map[string]string{}
	err := queryRows(ctx, d.sql, "team workdays",
		`SELECT day, name FROM team_holidays WHERE team_id = ? AND day >= ? AND day < ? AND kind = 'work' ORDER BY day`,
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
