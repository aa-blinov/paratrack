package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// GetSession fetches a session by id. A positive team ID constrains the read
// to that workspace; zero is reserved for legacy and actor-scoped tests.
func (d *DB) GetSession(ctx context.Context, query appmodel.SessionLookupQuery) (model.Session, error) {
	teamID, id := query.TeamID, query.SessionID
	if id <= 0 {
		return model.Session{}, ErrNotFound
	}
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

// ListClosedSessions reads closed sessions with optional activity or project filters.
func (d *DB) ListClosedSessions(ctx context.Context, query appmodel.ClosedSessionsQuery) ([]model.ActiveSession, error) {
	if query.TeamID <= 0 || query.Start.IsZero() || query.End.Before(query.Start) ||
		(query.ActivityID != nil && *query.ActivityID <= 0) || (query.ProjectID != nil && *query.ProjectID <= 0) {
		return nil, ErrNotFound
	}
	q := sessionSelect + `
		WHERE s.end_at IS NOT NULL
		  AND s.start_at <= ?
		  AND s.end_at   >= ?
		  AND s.team_id = ?`
	args := []any{FormatTime(query.End), FormatTime(query.Start), query.TeamID}
	if query.ActivityID != nil {
		q += ` AND s.activity_id = ?`
		args = append(args, *query.ActivityID)
	}
	if query.ProjectID != nil {
		q += ` AND a.project_id = ?`
		args = append(args, *query.ProjectID)
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

const sessionSelect = `
SELECT s.id, s.activity_id, s.team_id, s.user_id, s.start_at, s.end_at, s.note,
       s.paused, s.paused_at, s.accumulated_seconds, s.last_resume_at,
       s.created_at, s.updated_at,
       a.name AS activity_name, a.project_id AS activity_project_id
FROM sessions s
JOIN activities a ON a.id = s.activity_id AND a.team_id IS NOT DISTINCT FROM s.team_id`

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
		s            model.Session
		teamID       sql.NullInt64
		userID       sql.NullInt64
		startAt      string
		endAt        sql.NullString
		note         sql.NullString
		paused       int
		pausedAt     sql.NullString
		lastResumeAt sql.NullString
		createdAt    string
		updatedAt    string
		activityName string
		activityPID  sql.NullInt64
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
	start, err := ScanTime(startAt)
	if err != nil {
		return model.Session{}, "", 0, fmt.Errorf("parse session start: %w", err)
	}
	s.StartAt = start
	if endAt.Valid {
		t, err := ScanTime(endAt.String)
		if err != nil {
			return model.Session{}, "", 0, fmt.Errorf("parse session end: %w", err)
		}
		s.EndAt = &t
	}
	if note.Valid {
		v := note.String
		s.Note = &v
	}
	s.Paused = paused != 0
	if pausedAt.Valid {
		t, err := ScanTime(pausedAt.String)
		if err != nil {
			return model.Session{}, "", 0, fmt.Errorf("parse session pause time: %w", err)
		}
		s.PausedAt = &t
	}
	if lastResumeAt.Valid {
		t, err := ScanTime(lastResumeAt.String)
		if err != nil {
			return model.Session{}, "", 0, fmt.Errorf("parse session resume time: %w", err)
		}
		s.LastResumeAt = &t
	}
	s.CreatedAt, err = ScanTime(createdAt)
	if err != nil {
		return model.Session{}, "", 0, fmt.Errorf("parse session creation time: %w", err)
	}
	s.UpdatedAt, err = ScanTime(updatedAt)
	if err != nil {
		return model.Session{}, "", 0, fmt.Errorf("parse session update time: %w", err)
	}
	var pid int64
	if activityPID.Valid {
		pid = activityPID.Int64
	}
	return s, activityName, pid, nil
}

// SessionCursor is where a page of sessions stopped: the last row's start
// and id (the list is newest first, ties broken by id).
type SessionCursor = appmodel.SessionCursor

// ListSessionsPage is one page of the sessions touching [from, to]:
// closed ones overlapping it and running ones started inside it, newest
// first, at most limit rows after the cursor. more says whether another
// page follows.
func (d *DB) ListSessionsPage(ctx context.Context, query appmodel.SessionHistoryPageQuery) (list []model.ActiveSession, more bool, err error) {
	if query.TeamID <= 0 || query.From.IsZero() || query.To.IsZero() || query.To.Before(query.From) || query.Limit <= 0 ||
		(query.After != nil && (query.After.Start == "" || query.After.ID <= 0)) {
		return nil, false, ErrNotFound
	}
	q := sessionSelect + `
		WHERE s.team_id = ?
		  AND ((s.end_at IS NOT NULL AND s.start_at <= ? AND s.end_at >= ?)
	    OR (s.end_at IS NULL AND s.start_at >= ? AND s.start_at < ?))`
	args := []any{query.TeamID, FormatTime(query.To), FormatTime(query.From), FormatTime(query.From), FormatTime(query.To)}
	if query.After != nil {
		// "C" order: the ISO strings sort byte by byte, as time does.
		q += ` AND (s.start_at COLLATE "C" < ? OR (s.start_at = ? AND s.id < ?))`
		args = append(args, query.After.Start, query.After.Start, query.After.ID)
	}
	sc, args := scopeSQL(ctx, "s.user_id", args)
	q += sc + ` ORDER BY s.start_at COLLATE "C" DESC, s.id DESC LIMIT ?`
	args = append(args, query.Limit+1)
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	list, err = scanActiveSessions(rows)
	if err != nil {
		return nil, false, err
	}
	if len(list) > query.Limit {
		return list[:query.Limit], true, nil
	}
	return list, false, nil
}
