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
func (d *DB) CreateSession(ctx context.Context, activityID int64, startAt time.Time, note string) (model.Session, error) {
	startStr := FormatTime(startAt)
	res, err := d.sql.ExecContext(ctx,
		`INSERT INTO sessions (activity_id, start_at, note, paused, accumulated_seconds, last_resume_at)
		 VALUES (?, ?, ?, 0, 0, ?)`,
		activityID, startStr, nullableString(note), startStr,
	)
	if err != nil {
		return model.Session{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.Session{}, err
	}
	return d.GetSession(ctx, id)
}

// CreateClosedSession inserts a finished session in one go. Used by the
// `add` command for back-filling past intervals.
func (d *DB) CreateClosedSession(ctx context.Context, activityID int64, startAt, endAt time.Time, note string) (model.Session, error) {
	startStr := FormatTime(startAt)
	endStr := FormatTime(endAt)
	res, err := d.sql.ExecContext(ctx,
		`INSERT INTO sessions (activity_id, start_at, end_at, note, paused, accumulated_seconds, last_resume_at)
		 VALUES (?, ?, ?, ?, 0, 0, NULL)`,
		activityID, startStr, endStr, nullableString(note),
	)
	if err != nil {
		return model.Session{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.Session{}, err
	}
	return d.GetSession(ctx, id)
}

// GetSession fetches a session by id.
func (d *DB) GetSession(ctx context.Context, id int64) (model.Session, error) {
	row := d.sql.QueryRowContext(ctx, sessionSelect+` WHERE s.id = ?`, id)
	return scanSession(row)
}

// ListActiveSessions returns all sessions with end_at IS NULL, newest first,
// with their activity name attached for display.
func (d *DB) ListActiveSessions(ctx context.Context) ([]model.ActiveSession, error) {
	rows, err := d.sql.QueryContext(ctx, sessionSelect+`
		WHERE s.end_at IS NULL
		ORDER BY s.start_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanActiveSessions(rows)
}

// ListClosedSessionsInRange returns finished sessions whose interval
// overlaps [start, end]. Activity filter is optional.
func (d *DB) ListClosedSessionsInRange(ctx context.Context, start, end time.Time, activityID *int64) ([]model.ActiveSession, error) {
	q := sessionSelect + `
		WHERE s.end_at IS NOT NULL
		  AND s.start_at <= ?
		  AND s.end_at   >= ?`
	args := []any{FormatTime(end), FormatTime(start)}
	if activityID != nil {
		q += ` AND s.activity_id = ?`
		args = append(args, *activityID)
	}
	q += ` ORDER BY s.start_at DESC`
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanActiveSessions(rows)
}

// UpdateSessionEnd stops the session with a wall-clock end_at.
func (d *DB) UpdateSessionEnd(ctx context.Context, id int64, endAt time.Time) (model.Session, error) {
	_, err := d.sql.ExecContext(ctx,
		`UPDATE sessions SET end_at = ?, updated_at = ? WHERE id = ?`,
		FormatTime(endAt), FormatTime(time.Now().UTC()), id,
	)
	if err != nil {
		return model.Session{}, err
	}
	return d.GetSession(ctx, id)
}

// PauseSession rolls the running time into accumulated_seconds and flips
// the paused flag. Uses a single UPDATE for atomicity.
func (d *DB) PauseSession(ctx context.Context, id int64, now time.Time) (model.Session, error) {
	s, err := d.GetSession(ctx, id)
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
	_, err = d.sql.ExecContext(ctx,
		`UPDATE sessions
		 SET paused = 1,
		     paused_at = ?,
		     accumulated_seconds = ?,
		     last_resume_at = NULL,
		     updated_at = ?
		 WHERE id = ?`,
		nowStr, newAcc, FormatTime(time.Now().UTC()), id,
	)
	if err != nil {
		return model.Session{}, err
	}
	return d.GetSession(ctx, id)
}

// ResumeSession flips paused → false and sets last_resume_at = now.
func (d *DB) ResumeSession(ctx context.Context, id int64, now time.Time) (model.Session, error) {
	s, err := d.GetSession(ctx, id)
	if err != nil {
		return model.Session{}, err
	}
	if s.EndAt != nil || !s.Paused {
		return s, nil
	}
	nowStr := FormatTime(now)
	_, err = d.sql.ExecContext(ctx,
		`UPDATE sessions
		 SET paused = 0,
		     paused_at = NULL,
		     last_resume_at = ?,
		     updated_at = ?
		 WHERE id = ?`,
		nowStr, FormatTime(time.Now().UTC()), id,
	)
	if err != nil {
		return model.Session{}, err
	}
	return d.GetSession(ctx, id)
}

// DeleteSession removes a session by id (FK cascades handle session_tags).
func (d *DB) DeleteSession(ctx context.Context, id int64) error {
	_, err := d.sql.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

const sessionSelect = `
SELECT s.id, s.activity_id, s.start_at, s.end_at, s.note,
       s.paused, s.paused_at, s.accumulated_seconds, s.last_resume_at,
       s.created_at, s.updated_at,
       a.name AS activity_name
FROM sessions s
JOIN activities a ON a.id = s.activity_id`

func scanActiveSessions(rows *sql.Rows) ([]model.ActiveSession, error) {
	var out []model.ActiveSession
	for rows.Next() {
		s, name, err := scanSessionWithActivity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, model.ActiveSession{Session: s, Activity: model.Activity{ID: s.ActivityID, Name: name}})
	}
	return out, rows.Err()
}

func scanSession(r row) (model.Session, error) {
	s, _, err := scanSessionWithActivity(r)
	return s, err
}

func scanSessionWithActivity(r row) (model.Session, string, error) {
	var (
		s            model.Session
		startAt      string
		endAt        sql.NullString
		note         sql.NullString
		paused       int
		pausedAt     sql.NullString
		lastResumeAt sql.NullString
		createdAt    string
		updatedAt    string
		activityName string
	)
	if err := r.Scan(
		&s.ID, &s.ActivityID, &startAt, &endAt, &note,
		&paused, &pausedAt, &s.AccumulatedSeconds, &lastResumeAt,
		&createdAt, &updatedAt, &activityName,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Session{}, "", ErrNotFound
		}
		return model.Session{}, "", err
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
	return s, activityName, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
