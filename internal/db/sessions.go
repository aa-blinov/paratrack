package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

// CreateSession inserts a new session in active state (paused = 0,
// last_resume_at = start_at so the live timer starts ticking immediately).
// teamID is the workspace this session is filed under; pass 0 to skip
// the team association (legacy / tests).
func (d *DB) CreateSession(ctx context.Context, teamID, activityID int64, startAt time.Time, note string) (model.Session, error) {
	startStr := FormatTime(startAt)
	var id int64
	err := d.sql.QueryRowContext(ctx,
		`INSERT INTO sessions (activity_id, team_id, start_at, note, paused, accumulated_seconds, last_resume_at, user_id)
		 VALUES (?, ?, ?, ?, 0, 0, ?, ?) RETURNING id`,
		activityID, nullableInt64(teamID), startStr, nullableString(note), startStr, actorOf(ctx),
	).Scan(&id)
	if err != nil {
		return model.Session{}, err
	}
	if err != nil {
		return model.Session{}, err
	}
	return d.GetSession(ctx, teamID, id)
}

// CreateClosedSession inserts a finished session in one go. Used by the
// `add` command for back-filling past intervals.
// ImportedSessionExists reports whether an entry from another tracker
// (externalID "toggl:123") is already in the workspace.
func (d *DB) ImportedSessionExists(ctx context.Context, teamID int64, externalID string) bool {
	var one int
	return d.sql.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE team_id = ? AND external_id = ?`, teamID, externalID).Scan(&one) == nil
}

// MarkImported stamps a session with the id it had in the source tracker.
func (d *DB) MarkImported(ctx context.Context, teamID, sessionID int64, externalID string) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE sessions SET external_id = ? WHERE id = ? AND team_id = ?`, externalID, sessionID, teamID)
	return err
}

func (d *DB) CreateClosedSession(ctx context.Context, teamID, activityID int64, startAt, endAt time.Time, note string) (model.Session, error) {
	startStr := FormatTime(startAt)
	endStr := FormatTime(endAt)
	var id int64
	err := d.sql.QueryRowContext(ctx,
		`INSERT INTO sessions (activity_id, team_id, start_at, end_at, note, paused, accumulated_seconds, last_resume_at, user_id)
		 VALUES (?, ?, ?, ?, ?, 0, ?, NULL, ?) RETURNING id`,
		activityID, nullableInt64(teamID), startStr, endStr, nullableString(note),
		max(0, int(endAt.Sub(startAt).Seconds())), actorOf(ctx), // a closed session carries its tracked total
	).Scan(&id)
	if err != nil {
		return model.Session{}, err
	}
	if err != nil {
		return model.Session{}, err
	}
	return d.GetSession(ctx, teamID, id)
}

// GetSession fetches a session by id. Pass teamID > 0 to require the
// session to belong to that workspace (0 = legacy / CLI / tests).
func (d *DB) GetSession(ctx context.Context, teamID, id int64) (model.Session, error) {
	q := sessionSelect + ` WHERE s.id = ?`
	args := []any{id}
	var sc string
	sc, args = scopeSQL(ctx, "s.user_id", args)
	q += sc
	if teamID > 0 {
		q += ` AND s.team_id = ?`
		args = append(args, teamID)
	}
	row := d.sql.QueryRowContext(ctx, q, args...)
	return scanSession(row)
}

// HasAnySession reports whether the workspace has ever tracked anything;
// the dashboard's first-run state hangs off it.
func (d *DB) HasAnySession(ctx context.Context, teamID int64) (bool, error) {
	q := `SELECT 1 FROM sessions WHERE 1 = 1`
	var args []any
	if teamID > 0 {
		q += ` AND team_id = ?`
		args = append(args, teamID)
	}
	var sc string
	sc, args = scopeSQL(ctx, "user_id", args)
	q += sc
	var one int
	err := d.sql.QueryRowContext(ctx, q+` LIMIT 1`, args...).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// ListActiveSessions returns all sessions with end_at IS NULL, newest
// first, scoped to teamID (0 means "all teams" / legacy).
func (d *DB) ListActiveSessions(ctx context.Context, teamID int64) ([]model.ActiveSession, error) {
	q := sessionSelect + ` WHERE s.end_at IS NULL`
	args := []any{}
	var sc string
	sc, args = scopeSQL(ctx, "s.user_id", args)
	q += sc
	if teamID > 0 {
		q += ` AND s.team_id = ?`
		args = append(args, teamID)
	}
	q += ` ORDER BY s.start_at DESC`
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanActiveSessions(rows)
}

// ListClosedSessionsInRange returns finished sessions whose interval
// overlaps [start, end]. Activity filter is optional.
func (d *DB) ListClosedSessionsInRange(ctx context.Context, teamID int64, start, end time.Time, activityID *int64) ([]model.ActiveSession, error) {
	q := sessionSelect + `
		WHERE s.end_at IS NOT NULL
		  AND s.start_at <= ?
		  AND s.end_at   >= ?`
	args := []any{FormatTime(end), FormatTime(start)}
	if teamID > 0 {
		q += ` AND s.team_id = ?`
		args = append(args, teamID)
	}
	if activityID != nil {
		q += ` AND s.activity_id = ?`
		args = append(args, *activityID)
	}
	var sc string
	sc, args = scopeSQL(ctx, "s.user_id", args)
	q += sc + ` ORDER BY s.start_at DESC`
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanActiveSessions(rows)
}

// UpdateSessionEnd stops the session with a wall-clock end_at. Any
// time since the last resume is folded into accumulated_seconds first,
// so DurationSeconds() keeps working after close (pause gaps stay
// excluded). teamID > 0 restricts the write to that workspace.
func (d *DB) UpdateSessionEnd(ctx context.Context, teamID, id int64, endAt time.Time) (model.Session, error) {
	s, err := d.GetSession(ctx, teamID, id)
	if err != nil {
		return model.Session{}, err
	}
	newAcc := s.AccumulatedSeconds
	if !s.Paused {
		anchor := s.LastResumeAt
		if anchor == nil {
			anchor = &s.StartAt
		}
		elapsed := int(endAt.Sub(*anchor).Seconds())
		if elapsed > 0 {
			newAcc += elapsed
		}
	}
	q := `UPDATE sessions SET end_at = ?, accumulated_seconds = ?, updated_at = ? WHERE id = ?`
	args := []any{FormatTime(endAt), newAcc, FormatTime(time.Now().UTC()), id}
	if teamID > 0 {
		q += ` AND team_id = ?`
		args = append(args, teamID)
	}
	res, err := d.sql.ExecContext(ctx, q, args...)
	if err != nil {
		return model.Session{}, err
	}
	if teamID > 0 {
		if n, _ := res.RowsAffected(); n == 0 {
			return model.Session{}, ErrNotFound
		}
	}
	return d.GetSession(ctx, teamID, id)
}

// PauseSession rolls the running time into accumulated_seconds and flips
// the paused flag. Uses a single UPDATE for atomicity.
func (d *DB) PauseSession(ctx context.Context, teamID, id int64, now time.Time) (model.Session, error) {
	s, err := d.GetSession(ctx, teamID, id)
	if err != nil {
		return model.Session{}, err
	}
	if s.EndAt != nil || s.Paused {
		return s, nil // idempotent
	}
	anchor := s.LastResumeAt
	if anchor == nil {
		anchor = &s.StartAt
	}
	elapsed := int(now.Sub(*anchor).Seconds())
	if elapsed < 0 {
		elapsed = 0
	}
	newAcc := s.AccumulatedSeconds + elapsed
	nowStr := FormatTime(now)
	q := `UPDATE sessions
		 SET paused = 1,
		     paused_at = ?,
		     accumulated_seconds = ?,
		     last_resume_at = NULL,
		     updated_at = ?
		 WHERE id = ?`
	args := []any{nowStr, newAcc, FormatTime(time.Now().UTC()), id}
	if teamID > 0 {
		q += ` AND team_id = ?`
		args = append(args, teamID)
	}
	_, err = d.sql.ExecContext(ctx, q, args...)
	if err != nil {
		return model.Session{}, err
	}
	return d.GetSession(ctx, teamID, id)
}

// ResumeSession flips paused → false and sets last_resume_at = now.
func (d *DB) ResumeSession(ctx context.Context, teamID, id int64, now time.Time) (model.Session, error) {
	s, err := d.GetSession(ctx, teamID, id)
	if err != nil {
		return model.Session{}, err
	}
	if s.EndAt != nil || !s.Paused {
		return s, nil
	}
	nowStr := FormatTime(now)
	q := `UPDATE sessions
		 SET paused = 0,
		     paused_at = NULL,
		     last_resume_at = ?,
		     updated_at = ?
		 WHERE id = ?`
	args := []any{nowStr, FormatTime(time.Now().UTC()), id}
	if teamID > 0 {
		q += ` AND team_id = ?`
		args = append(args, teamID)
	}
	_, err = d.sql.ExecContext(ctx, q, args...)
	if err != nil {
		return model.Session{}, err
	}
	return d.GetSession(ctx, teamID, id)
}

// ReopenSession undoes a stop: clears end_at and, for a session that was
// running, resumes it from the stop moment so the undo leaves no gap.
// accumulated_seconds already holds the time folded in at stop.
func (d *DB) ReopenSession(ctx context.Context, teamID, id int64) (model.Session, error) {
	s, err := d.GetSession(ctx, teamID, id)
	if err != nil {
		return model.Session{}, err
	}
	if s.EndAt == nil {
		return s, nil
	}
	q := `UPDATE sessions SET end_at = NULL, updated_at = ?`
	args := []any{FormatTime(time.Now().UTC())}
	if !s.Paused {
		q += `, last_resume_at = ?`
		args = append(args, FormatTime(*s.EndAt))
	}
	q += ` WHERE id = ?`
	args = append(args, id)
	if teamID > 0 {
		q += ` AND team_id = ?`
		args = append(args, teamID)
	}
	if _, err := d.sql.ExecContext(ctx, q, args...); err != nil {
		return model.Session{}, err
	}
	return d.GetSession(ctx, teamID, id)
}

// DeleteSession removes a session by id (FK cascades handle session_tags).
// teamID > 0 restricts the delete to that workspace.
func (d *DB) DeleteSession(ctx context.Context, teamID, id int64) error {
	q := `DELETE FROM sessions WHERE id = ?`
	args := []any{id}
	if teamID > 0 {
		q += ` AND team_id = ?`
		args = append(args, teamID)
	}
	var sc string
	sc, args = scopeSQL(ctx, "user_id", args)
	q += sc
	res, err := d.sql.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if teamID > 0 {
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
	}
	return nil
}

const sessionSelect = `
SELECT s.id, s.activity_id, s.team_id, s.user_id, s.start_at, s.end_at, s.note,
       s.paused, s.paused_at, s.accumulated_seconds, s.last_resume_at,
       s.created_at, s.updated_at,
       a.name AS activity_name, a.project_id AS activity_project_id
FROM sessions s
JOIN activities a ON a.id = s.activity_id`

func scanActiveSessions(rows *sql.Rows) ([]model.ActiveSession, error) {
	var out []model.ActiveSession
	for rows.Next() {
		s, name, projectID, err := scanSessionWithActivity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, model.ActiveSession{
			Session:  s,
			Activity: model.Activity{ID: s.ActivityID, Name: name, ProjectID: projectID},
		})
	}
	return out, rows.Err()
}

func scanSession(r row) (model.Session, error) {
	s, _, _, err := scanSessionWithActivity(r)
	return s, err
}

func scanSessionWithActivity(r row) (model.Session, string, int64, error) {
	var (
		s              model.Session
		teamID         sql.NullInt64
		userID         sql.NullInt64
		startAt        string
		endAt          sql.NullString
		note           sql.NullString
		paused         int
		pausedAt       sql.NullString
		lastResumeAt   sql.NullString
		createdAt      string
		updatedAt      string
		activityName   string
		activityPID    sql.NullInt64
	)
	if err := r.Scan(
		&s.ID, &s.ActivityID, &teamID, &userID, &startAt, &endAt, &note,
		&paused, &pausedAt, &s.AccumulatedSeconds, &lastResumeAt,
		&createdAt, &updatedAt, &activityName, &activityPID,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Session{}, "", 0, ErrNotFound
		}
		return model.Session{}, "", 0, err
	}
	if teamID.Valid {
		s.TeamID = teamID.Int64
	}
	if userID.Valid {
		s.UserID = userID.Int64
	}
	if t, err := ScanTime(startAt); err == nil {
		s.StartAt = t
	}
	if endAt.Valid {
		if t, err := ScanTime(endAt.String); err == nil {
			s.EndAt = &t
		}
	}
	if note.Valid {
		v := note.String
		s.Note = &v
	}
	s.Paused = paused != 0
	if pausedAt.Valid {
		if t, err := ScanTime(pausedAt.String); err == nil {
			s.PausedAt = &t
		}
	}
	if lastResumeAt.Valid {
		if t, err := ScanTime(lastResumeAt.String); err == nil {
			s.LastResumeAt = &t
		}
	}
	if t, err := ScanTime(createdAt); err == nil {
		s.CreatedAt = t
	}
	if t, err := ScanTime(updatedAt); err == nil {
		s.UpdatedAt = t
	}
	var pid int64
	if activityPID.Valid {
		pid = activityPID.Int64
	}
	return s, activityName, pid, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullableInt64 returns nil for 0 so callers can store NULL in the
// nullable team_id column. Used by CreateSession / CreateClosedSession.
func nullableInt64(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}
// SessionCursor is where a page of sessions stopped: the last row's start
// and id (the list is newest first, ties broken by id).
type SessionCursor struct {
	Start string
	ID    int64
}

// ListSessionsPage is one page of the sessions touching [from, to]:
// closed ones overlapping it and running ones started inside it, newest
// first, at most limit rows after the cursor. more says whether another
// page follows.
func (d *DB) ListSessionsPage(ctx context.Context, teamID int64, from, to time.Time, after *SessionCursor, limit int) (list []model.ActiveSession, more bool, err error) {
	q := sessionSelect + `
		WHERE s.team_id = ?
		  AND ((s.end_at IS NOT NULL AND s.start_at <= ? AND s.end_at >= ?)
		    OR (s.end_at IS NULL AND s.start_at >= ? AND s.start_at < ?))`
	args := []any{teamID, FormatTime(to), FormatTime(from), FormatTime(from), FormatTime(to)}
	if after != nil {
		// "C" order: the ISO strings sort byte by byte, as time does.
		q += ` AND (s.start_at COLLATE "C" < ? OR (s.start_at = ? AND s.id < ?))`
		args = append(args, after.Start, after.Start, after.ID)
	}
	sc, args := scopeSQL(ctx, "s.user_id", args)
	q += sc + ` ORDER BY s.start_at COLLATE "C" DESC, s.id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	list, err = scanActiveSessions(rows)
	if err != nil {
		return nil, false, err
	}
	if len(list) > limit {
		return list[:limit], true, nil
	}
	return list, false, nil
}
