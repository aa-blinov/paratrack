package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
)

// ScheduleRow is one member's planned work during a week.
type ScheduleRow = appmodel.ScheduleRow

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
	if err := authorizeSchedulePlan(ctx, tx, teamID, actorID, projectID); err != nil {
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
	tracked, err := loadScheduleTracked(ctx, tx, query)
	if err != nil {
		return nil, nil, fmt.Errorf("load schedule tracked time: %w", err)
	}
	rows := buildScheduleRows(users, entries, byProject, query.WeekStart)
	for i := range rows {
		rows[i].Tracked = tracked[rows[i].UserID]
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit schedule snapshot: %w", err)
	}
	return rows, projectNames, nil
}

type scheduleUser struct {
	id    int64
	name  string
	cap   int
	admin int
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
		`SELECT u.id, u.name, u.email, COALESCE(m.capacity_minutes, 0), COALESCE(m.admin_minutes, 0)
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
		if err := rows.Scan(&user.id, &user.name, &email, &user.cap, &user.admin); err != nil {
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

// loadScheduleTracked sums the minutes each member logged in the week, open
// timers included, the same way the timesheet counts them. It reads the whole
// team: the handler shows the column to managers only.
func loadScheduleTracked(ctx context.Context, tx *Tx, query appmodel.ScheduleQuery) (map[int64]int, error) {
	weekEnd := query.WeekStart.AddDate(0, 0, 7)
	rows, err := tx.QueryContext(ctx,
		`SELECT user_id, start_at, end_at, accumulated_seconds, paused, last_resume_at
		 FROM sessions
		 WHERE team_id = ? AND start_at < ? AND (end_at IS NULL OR end_at >= ?)`,
		query.TeamID, FormatTime(weekEnd), FormatTime(query.WeekStart))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seconds := make(map[int64]int)
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
		started, err := ScanTime(startAt)
		if err != nil {
			return nil, fmt.Errorf("parse schedule session start: %w", err)
		}
		session := model.Session{StartAt: started, AccumulatedSeconds: accumulated, Paused: paused == 1}
		if endAt.Valid {
			ended, err := ScanTime(endAt.String)
			if err != nil {
				return nil, fmt.Errorf("parse schedule session end: %w", err)
			}
			session.EndAt = &ended
		}
		if lastResumeAt.Valid {
			resumed, err := ScanTime(lastResumeAt.String)
			if err != nil {
				return nil, fmt.Errorf("parse schedule session resume time: %w", err)
			}
			session.LastResumeAt = &resumed
		}
		total, err := money.AddInt(seconds[userID.Int64], session.TrackedSecondsInWindow(query.WeekStart, weekEnd, query.Now))
		if err != nil {
			return nil, fmt.Errorf("sum schedule tracked time: %w", err)
		}
		seconds[userID.Int64] = total
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	minutes := make(map[int64]int, len(seconds))
	for userID, sec := range seconds {
		minutes[userID] = sec / 60
	}
	return minutes, nil
}

func buildScheduleRows(users []scheduleUser, entries map[scheduleCellKey]int, byProject map[scheduleProjectCellKey]int, weekStart time.Time) []ScheduleRow {
	rows := make([]ScheduleRow, len(users))
	rowByUser := make(map[int64]int, len(users))
	dayIndex := make(map[string]int, 7)
	for i := 0; i < 7; i++ {
		dayIndex[scheduleDay(weekStart.AddDate(0, 0, i))] = i
	}
	for i, user := range users {
		rows[i] = ScheduleRow{UserID: user.id, UserName: user.name, Capacity: user.cap, AdminMinutes: user.admin, ByProject: make(map[int64][7]int)}
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

// PlannedMinutesByUser sums each member's planned minutes over the days in
// [from, to). Days compare as their YYYY-MM-DD text, like the week grid does.
func (d *DB) PlannedMinutesByUser(ctx context.Context, teamID int64, from, to time.Time) (map[int64]int, error) {
	if teamID <= 0 {
		return nil, ErrNotFound
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT user_id, COALESCE(SUM(minutes), 0) FROM schedule_entries
		 WHERE team_id = ? AND day >= ? AND day < ? GROUP BY user_id`,
		teamID, scheduleDay(from), scheduleDay(to))
	if err != nil {
		return nil, fmt.Errorf("sum planned minutes: %w", err)
	}
	defer rows.Close()
	planned := make(map[int64]int)
	for rows.Next() {
		var userID int64
		var minutes int
		if err := rows.Scan(&userID, &minutes); err != nil {
			return nil, err
		}
		planned[userID] = minutes
	}
	return planned, rows.Err()
}

// CopyScheduleWeek repeats every planned cell of the source week seven days
// later. The target week is checked inside the same transaction as the copy,
// so a cell written meanwhile cannot be overwritten.
func (d *DB) CopyScheduleWeek(ctx context.Context, request appmodel.ScheduleCopyRequest) error {
	teamID := request.TeamID
	if teamID <= 0 || request.ActorID <= 0 || request.From.IsZero() || request.To.IsZero() {
		return ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, teamID, request.ActorID); err != nil {
		return err
	}
	var lockedID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM teams WHERE id = ? FOR UPDATE`, teamID).Scan(&lockedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	var targetCells int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schedule_entries WHERE team_id = ? AND day >= ? AND day < ?`,
		teamID, scheduleDay(request.To), scheduleDay(request.To.AddDate(0, 0, 7))).Scan(&targetCells); err != nil {
		return err
	}
	if targetCells > 0 {
		return appmodel.ErrScheduleWeekNotEmpty
	}
	sourceRows, err := tx.QueryContext(ctx,
		`SELECT user_id, project_id, day, minutes FROM schedule_entries WHERE team_id = ? AND day >= ? AND day < ?`,
		teamID, scheduleDay(request.From), scheduleDay(request.From.AddDate(0, 0, 7)))
	if err != nil {
		return err
	}
	type cell struct {
		userID, projectID int64
		day               string
		minutes           int
	}
	var cells []cell
	for sourceRows.Next() {
		var c cell
		if err := sourceRows.Scan(&c.userID, &c.projectID, &c.day, &c.minutes); err != nil {
			_ = sourceRows.Close()
			return err
		}
		cells = append(cells, c)
	}
	if err := sourceRows.Err(); err != nil {
		_ = sourceRows.Close()
		return err
	}
	if err := sourceRows.Close(); err != nil {
		return err
	}
	if len(cells) == 0 {
		return appmodel.ErrScheduleNothingToCopy
	}
	now := FormatTime(d.currentTime().UTC())
	for _, c := range cells {
		day, err := time.Parse("2006-01-02", c.day)
		if err != nil {
			return fmt.Errorf("parse planned day %q: %w", c.day, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schedule_entries (team_id, user_id, project_id, day, minutes, note, created_at)
			 VALUES (?, ?, ?, ?, ?, '', ?)
			 ON CONFLICT (team_id, user_id, project_id, day) DO UPDATE SET minutes = excluded.minutes`,
			teamID, c.userID, c.projectID, scheduleDay(day.AddDate(0, 0, 7)), c.minutes, now); err != nil {
			return fmt.Errorf("copy planned cell: %w", err)
		}
	}
	return tx.Commit()
}

// SetScheduleAdminMinutes stores a member's weekly admin time.
func (d *DB) SetScheduleAdminMinutes(ctx context.Context, request appmodel.ScheduleAdminRequest) error {
	if request.TeamID <= 0 || request.ActorID <= 0 || request.UserID <= 0 {
		return ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.ActorID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE memberships SET admin_minutes = ? WHERE team_id = ? AND user_id = ?`,
		request.Minutes, request.TeamID, request.UserID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// PlannedMinutesForProject sums each member's planned minutes on one project
// over every week it is planned in.
func (d *DB) PlannedMinutesForProject(ctx context.Context, teamID, projectID int64) (map[int64]int, error) {
	if teamID <= 0 || projectID <= 0 {
		return nil, ErrNotFound
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT user_id, COALESCE(SUM(minutes), 0) FROM schedule_entries WHERE team_id = ? AND project_id = ? GROUP BY user_id`,
		teamID, projectID)
	if err != nil {
		return nil, fmt.Errorf("sum project plan: %w", err)
	}
	defer rows.Close()
	planned := make(map[int64]int)
	for rows.Next() {
		var userID int64
		var minutes int
		if err := rows.Scan(&userID, &minutes); err != nil {
			return nil, err
		}
		planned[userID] = minutes
	}
	return planned, rows.Err()
}

// PlannedMinutesForProjectFrom sums the plan on one project from a day onward:
// what is still ahead of the team, for "left of the estimate".
func (d *DB) PlannedMinutesForProjectFrom(ctx context.Context, teamID, projectID int64, from time.Time) (int, error) {
	if teamID <= 0 || projectID <= 0 {
		return 0, ErrNotFound
	}
	var minutes int
	err := d.sql.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(minutes), 0) FROM schedule_entries WHERE team_id = ? AND project_id = ? AND day >= ?`,
		teamID, projectID, scheduleDay(from)).Scan(&minutes)
	if err != nil {
		return 0, fmt.Errorf("sum project plan ahead: %w", err)
	}
	return minutes, nil
}

// TrackedMinutesForProject sums the minutes each member logged on one project,
// open timers included, over the whole history up to now. The sessions are
// read in full: a team's history on one project is small enough for that.
func (d *DB) TrackedMinutesForProject(ctx context.Context, teamID, projectID int64, now time.Time) (map[int64]int, error) {
	if teamID <= 0 || projectID <= 0 {
		return nil, ErrNotFound
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT s.user_id, s.start_at, s.end_at, s.accumulated_seconds, s.paused, s.last_resume_at
		 FROM sessions s JOIN activities a ON a.id = s.activity_id AND a.team_id = s.team_id
		 WHERE s.team_id = ? AND a.project_id = ?`,
		teamID, projectID)
	if err != nil {
		return nil, fmt.Errorf("read project sessions: %w", err)
	}
	defer rows.Close()
	seconds := make(map[int64]int)
	from := time.Unix(0, 0).UTC()
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
		total, err := money.AddInt(seconds[userID.Int64], session.TrackedSecondsInWindow(from, now.AddDate(100, 0, 0), now))
		if err != nil {
			return nil, fmt.Errorf("sum project tracked time: %w", err)
		}
		seconds[userID.Int64] = total
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	minutes := make(map[int64]int, len(seconds))
	for userID, sec := range seconds {
		minutes[userID] = sec / 60
	}
	return minutes, nil
}

// scheduleSession rebuilds a stored session from its columns, the way the
// tracked-time readers do.
func scheduleSession(startAt string, endAt, lastResumeAt sql.NullString, accumulated, paused int) (model.Session, error) {
	started, err := ScanTime(startAt)
	if err != nil {
		return model.Session{}, fmt.Errorf("parse session start: %w", err)
	}
	session := model.Session{StartAt: started, AccumulatedSeconds: accumulated, Paused: paused == 1}
	if endAt.Valid {
		ended, err := ScanTime(endAt.String)
		if err != nil {
			return model.Session{}, fmt.Errorf("parse session end: %w", err)
		}
		session.EndAt = &ended
	}
	if lastResumeAt.Valid {
		resumed, err := ScanTime(lastResumeAt.String)
		if err != nil {
			return model.Session{}, fmt.Errorf("parse session resume time: %w", err)
		}
		session.LastResumeAt = &resumed
	}
	return session, nil
}

func scheduleDay(day time.Time) string { return day.Format("2006-01-02") }

// authorizeSchedulePlan lets a manager plan any project, and the lead of a
// project plan that project only. The lead must still be a member: a person
// who left the workspace keeps no say in its plan.
func authorizeSchedulePlan(ctx context.Context, tx *Tx, teamID, actorID, projectID int64) error {
	_, _, err := lockTeamManager(ctx, tx, teamID, actorID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, model.ErrForbidden) {
		return err
	}
	var lead sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT lead_user_id FROM projects WHERE id = ? AND team_id = ?`, projectID, teamID).Scan(&lead)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !lead.Valid || lead.Int64 != actorID {
		return model.ErrForbidden
	}
	if err := checkMemberInTeam(ctx, tx, actorID, teamID); err != nil {
		return model.ErrForbidden
	}
	return nil
}
