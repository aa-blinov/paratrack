package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

// ScheduleRow is one member's planned work during a week.
type ScheduleRow = model.ScheduleRow

// UpsertScheduleEntry sets planned minutes for one workspace schedule cell.
func (d *DB) UpsertScheduleEntry(ctx context.Context, request appmodel.ScheduleCellRequest) error {
	teamID, actorID, userID, projectID := request.TeamID, request.ActorID, request.UserID, request.ProjectID
	day, minutes, note := request.Day.Format("2006-01-02"), request.Minutes, request.Note
	if teamID <= 0 || actorID <= 0 || userID <= 0 || projectID <= 0 {
		return ErrNotFound
	}
	if request.Day.IsZero() {
		return fmt.Errorf("day is required")
	}
	if minutes < 0 || minutes > 24*60 {
		return fmt.Errorf("minutes must be 0..1440")
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, teamID, actorID); err != nil {
		return err
	}
	// Lock in the same team -> user order as member removal. This prevents
	// a schedule cell from being created for a member while that membership
	// is concurrently removed.
	var lockedID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM teams WHERE id = ? FOR UPDATE`, teamID).Scan(&lockedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id = ? FOR UPDATE`, userID).Scan(&lockedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if err := tx.QueryRowContext(ctx,
		`SELECT user_id FROM memberships WHERE team_id = ? AND user_id = ? FOR UPDATE`, teamID, userID).Scan(&lockedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM projects WHERE team_id = ? AND id = ? FOR UPDATE`, teamID, projectID).Scan(&lockedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	now := FormatTime(d.currentTime().UTC())
	if minutes == 0 {
		_, err := tx.ExecContext(ctx,
			`DELETE FROM schedule_entries WHERE team_id = ? AND user_id = ? AND project_id = ? AND day = ?`,
			teamID, userID, projectID, day)
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO schedule_entries (team_id, user_id, project_id, day, minutes, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (team_id, user_id, project_id, day) DO UPDATE SET
		  minutes = excluded.minutes,
		  note = excluded.note`,
		teamID, userID, projectID, day, minutes, note, now)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// ListSchedule returns the plan for a week plus a project breakdown.
// weekStart is the first day selected by the user's workspace preference.
func (d *DB) ListSchedule(ctx context.Context, query appmodel.ScheduleQuery) ([]ScheduleRow, map[int64]string, error) {
	if query.TeamID <= 0 || query.WeekStart.IsZero() {
		return nil, nil, ErrNotFound
	}
	weekEnd := query.WeekStart.AddDate(0, 0, 7)
	dayStr := func(t time.Time) string { return t.Format("2006-01-02") }

	// users in the team
	urows, err := d.sql.QueryContext(ctx,
		`SELECT u.id, u.name, u.email, COALESCE(m.capacity_minutes, 0)
		 FROM memberships m JOIN users u ON u.id = m.user_id
		 WHERE m.team_id = ? ORDER BY u.name`, query.TeamID)
	if err != nil {
		return nil, nil, err
	}
	defer urows.Close()
	type userRec struct {
		id   int64
		name string
		cap  int
	}
	var users []userRec
	for urows.Next() {
		var u userRec
		var email string
		if err := urows.Scan(&u.id, &u.name, &email, &u.cap); err != nil {
			urows.Close()
			return nil, nil, err
		}
		if u.name == "" {
			u.name = email
		}
		if u.cap <= 0 {
			u.cap = 480
		}
		users = append(users, u)
	}
	if err := urows.Err(); err != nil {
		urows.Close()
		return nil, nil, err
	}
	urows.Close()

	// project names
	pnames := map[int64]string{}
	prows, err := d.sql.QueryContext(ctx, `SELECT id, name FROM projects WHERE team_id = ?`, query.TeamID)
	if err != nil {
		return nil, nil, err
	}
	defer prows.Close()
	for prows.Next() {
		var id int64
		var name string
		if err := prows.Scan(&id, &name); err != nil {
			prows.Close()
			return nil, nil, err
		}
		pnames[id] = name
	}
	if err := prows.Err(); err != nil {
		prows.Close()
		return nil, nil, err
	}
	prows.Close()

	// entries in window
	q := `SELECT user_id, project_id, day, minutes FROM schedule_entries
	      WHERE team_id = ? AND day >= ? AND day < ?`
	erows, err := d.sql.QueryContext(ctx, q, query.TeamID, dayStr(query.WeekStart), dayStr(weekEnd))
	if err != nil {
		return nil, nil, err
	}
	defer erows.Close()
	type key struct {
		uid int64
		day string
	}
	type pkey struct {
		uid, pid int64
		day      string
	}
	entry := map[key]int{}
	byProj := map[pkey]int{}
	for erows.Next() {
		var uid, pid int64
		var day string
		var m int
		if err := erows.Scan(&uid, &pid, &day, &m); err != nil {
			erows.Close()
			return nil, nil, err
		}
		entry[key{uid, day}] += m
		byProj[pkey{uid, pid, day}] += m
	}
	if err := erows.Err(); err != nil {
		erows.Close()
		return nil, nil, err
	}
	erows.Close()

	rows := make([]ScheduleRow, 0, len(users))
	for _, u := range users {
		row := ScheduleRow{UserID: u.id, UserName: u.name, Capacity: u.cap, ByProject: map[int64][7]int{}}
		for i := 0; i < 7; i++ {
			day := dayStr(query.WeekStart.AddDate(0, 0, i))
			row.Minutes[i] = entry[key{u.id, day}]
			row.Total += row.Minutes[i]
		}
		for k, m := range byProj {
			if k.uid != u.id {
				continue
			}
			for i := 0; i < 7; i++ {
				if dayStr(query.WeekStart.AddDate(0, 0, i)) == k.day {
					w := row.ByProject[k.pid]
					w[i] += m
					row.ByProject[k.pid] = w
				}
			}
		}
		rows = append(rows, row)
	}
	return rows, pnames, nil
}
