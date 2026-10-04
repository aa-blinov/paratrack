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

// ListSchedule returns the plan for a week plus a project breakdown from one
// repeatable-read snapshot, so membership, project names and cells agree.
func (d *DB) ListSchedule(ctx context.Context, query appmodel.ScheduleQuery) ([]ScheduleRow, map[int64]string, error) {
	if query.TeamID <= 0 || query.WeekStart.IsZero() {
		return nil, nil, ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, nil, fmt.Errorf("begin schedule snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	users, err := loadScheduleUsers(ctx, tx, query.TeamID)
	if err != nil {
		return nil, nil, fmt.Errorf("load schedule members: %w", err)
	}
	projectNames, err := loadScheduleProjectNames(ctx, tx, query.TeamID)
	if err != nil {
		return nil, nil, fmt.Errorf("load schedule projects: %w", err)
	}
	entries, byProject, err := loadScheduleEntries(ctx, tx, query)
	if err != nil {
		return nil, nil, fmt.Errorf("load schedule entries: %w", err)
	}
	rows := buildScheduleRows(users, entries, byProject, query.WeekStart)
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit schedule snapshot: %w", err)
	}
	return rows, projectNames, nil
}

type scheduleUser struct {
	id   int64
	name string
	cap  int
}

type scheduleCellKey struct {
	userID int64
	day    string
}

type scheduleProjectCellKey struct {
	userID, projectID int64
	day               string
}

func loadScheduleUsers(ctx context.Context, tx *Tx, teamID int64) ([]scheduleUser, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT u.id, u.name, u.email, COALESCE(m.capacity_minutes, 0)
		 FROM memberships m JOIN users u ON u.id = m.user_id
		 WHERE m.team_id = ? ORDER BY u.name`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []scheduleUser
	for rows.Next() {
		var user scheduleUser
		var email string
		if err := rows.Scan(&user.id, &user.name, &email, &user.cap); err != nil {
			return nil, err
		}
		if user.name == "" {
			user.name = email
		}
		if user.cap <= 0 {
			user.cap = 480
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func loadScheduleProjectNames(ctx context.Context, tx *Tx, teamID int64) (map[int64]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, name FROM projects WHERE team_id = ?`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := make(map[int64]string)
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		names[id] = name
	}
	return names, rows.Err()
}

func loadScheduleEntries(ctx context.Context, tx *Tx, query appmodel.ScheduleQuery) (map[scheduleCellKey]int, map[scheduleProjectCellKey]int, error) {
	weekEnd := query.WeekStart.AddDate(0, 0, 7)
	rows, err := tx.QueryContext(ctx,
		`SELECT user_id, project_id, day, minutes FROM schedule_entries
		 WHERE team_id = ? AND day >= ? AND day < ?`,
		query.TeamID, scheduleDay(query.WeekStart), scheduleDay(weekEnd))
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	entries := make(map[scheduleCellKey]int)
	byProject := make(map[scheduleProjectCellKey]int)
	for rows.Next() {
		var userID, projectID int64
		var day string
		var minutes int
		if err := rows.Scan(&userID, &projectID, &day, &minutes); err != nil {
			return nil, nil, err
		}
		entries[scheduleCellKey{userID, day}] += minutes
		byProject[scheduleProjectCellKey{userID, projectID, day}] += minutes
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return entries, byProject, nil
}

func buildScheduleRows(users []scheduleUser, entries map[scheduleCellKey]int, byProject map[scheduleProjectCellKey]int, weekStart time.Time) []ScheduleRow {
	rows := make([]ScheduleRow, len(users))
	rowByUser := make(map[int64]int, len(users))
	dayIndex := make(map[string]int, 7)
	for i := 0; i < 7; i++ {
		dayIndex[scheduleDay(weekStart.AddDate(0, 0, i))] = i
	}
	for i, user := range users {
		rows[i] = ScheduleRow{UserID: user.id, UserName: user.name, Capacity: user.cap, ByProject: make(map[int64][7]int)}
		rowByUser[user.id] = i
	}
	for cell, minutes := range entries {
		rowIndex, exists := rowByUser[cell.userID]
		day, validDay := dayIndex[cell.day]
		if !exists || !validDay {
			continue
		}
		rows[rowIndex].Minutes[day] += minutes
	}
	for cell, minutes := range byProject {
		rowIndex, exists := rowByUser[cell.userID]
		day, validDay := dayIndex[cell.day]
		if !exists || !validDay {
			continue
		}
		projectMinutes := rows[rowIndex].ByProject[cell.projectID]
		projectMinutes[day] += minutes
		rows[rowIndex].ByProject[cell.projectID] = projectMinutes
	}
	for i := range rows {
		for _, minutes := range rows[i].Minutes {
			rows[i].Total += minutes
		}
	}
	return rows
}

func scheduleDay(day time.Time) string { return day.Format("2006-01-02") }
