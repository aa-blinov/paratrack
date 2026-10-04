package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/webhookport"
)

// UpdateSessionEnd stops the session with a wall-clock end_at. Any
// time since the last resume is folded into accumulated_seconds first,
// so DurationSeconds() keeps working after close (pause gaps stay
// excluded). teamID > 0 restricts the write to that workspace.
func (d *DB) UpdateSessionEnd(ctx context.Context, request appmodel.TimerStopRequest) (model.Session, error) {
	teamID, id, endAt := request.TeamID, request.SessionID, request.At
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.Session{}, err
	}
	defer tx.Rollback()
	if err := requireTeamMembership(ctx, tx, teamID); err != nil {
		return model.Session{}, err
	}
	s, err := getSessionTx(ctx, tx, teamID, id)
	if err != nil {
		return model.Session{}, err
	}
	if s.EndAt != nil {
		return model.Session{}, ErrSessionNotActive
	}
	newAcc := int64(s.AccumulatedSeconds)
	if !s.Paused {
		anchor := s.LastResumeAt
		if anchor == nil {
			anchor = &s.StartAt
		}
		elapsed := int64(endAt.Sub(*anchor).Seconds())
		if elapsed < 0 {
			elapsed = 0
		}
		newAcc, err = addSessionSeconds(newAcc, elapsed)
		if err != nil {
			return model.Session{}, fmt.Errorf("stop session %d: %w", id, err)
		}
	}
	q := `UPDATE sessions SET end_at = ?, accumulated_seconds = ?, updated_at = ? WHERE id = ? AND end_at IS NULL`
	args := []any{FormatTime(endAt), newAcc, FormatTime(endAt.UTC()), id}
	if teamID > 0 {
		q += ` AND team_id = ?`
		args = append(args, teamID)
	}
	res, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return model.Session{}, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return model.Session{}, err
	} else if n != 1 {
		return model.Session{}, ErrSessionNotActive
	}
	if err := d.recordWebhookEventTx(ctx, tx, teamID, webhookport.SessionStoppedEvent{SessionID: s.ID, ActivityID: s.ActivityID, Start: s.StartAt.UTC().Format(time.RFC3339)}, endAt); err != nil {
		return model.Session{}, err
	}
	updated, err := getSessionTx(ctx, tx, teamID, id)
	if err != nil {
		return model.Session{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Session{}, err
	}
	return updated, nil
}

// PauseSession rolls the running time into accumulated_seconds and flips
// the paused flag. Uses a single UPDATE for atomicity.
func (d *DB) PauseSession(ctx context.Context, request appmodel.TimerSessionRequest) (model.Session, error) {
	teamID, id, now := request.TeamID, request.SessionID, request.At
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.Session{}, err
	}
	defer tx.Rollback()
	if err := requireTeamMembership(ctx, tx, teamID); err != nil {
		return model.Session{}, err
	}
	s, err := getSessionTx(ctx, tx, teamID, id)
	if err != nil {
		return model.Session{}, err
	}
	if s.EndAt != nil || s.Paused {
		if err := tx.Commit(); err != nil {
			return model.Session{}, err
		}
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
	newAcc, err := addSessionSeconds(int64(s.AccumulatedSeconds), int64(elapsed))
	if err != nil {
		return model.Session{}, fmt.Errorf("pause session %d: %w", id, err)
	}
	nowStr := FormatTime(now)
	q := `UPDATE sessions
		 SET paused = 1,
		     paused_at = ?,
		     accumulated_seconds = ?,
		     last_resume_at = NULL,
		     updated_at = ?
		 WHERE id = ? AND end_at IS NULL AND paused = 0`
	args := []any{nowStr, newAcc, FormatTime(now.UTC()), id}
	if teamID > 0 {
		q += ` AND team_id = ?`
		args = append(args, teamID)
	}
	res, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return model.Session{}, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return model.Session{}, err
	} else if n != 1 {
		return model.Session{}, ErrNotFound
	}
	updated, err := getSessionTx(ctx, tx, teamID, id)
	if err != nil {
		return model.Session{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Session{}, err
	}
	return updated, nil
}

// ResumeSession flips paused → false and sets last_resume_at = now.
func (d *DB) ResumeSession(ctx context.Context, request appmodel.TimerSessionRequest) (model.Session, error) {
	teamID, id, now := request.TeamID, request.SessionID, request.At
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.Session{}, err
	}
	defer tx.Rollback()
	if err := requireTeamMembership(ctx, tx, teamID); err != nil {
		return model.Session{}, err
	}
	s, err := getSessionTx(ctx, tx, teamID, id)
	if err != nil {
		return model.Session{}, err
	}
	if s.EndAt != nil || !s.Paused {
		if err := tx.Commit(); err != nil {
			return model.Session{}, err
		}
		return s, nil
	}
	nowStr := FormatTime(now)
	q := `UPDATE sessions
		 SET paused = 0,
		     paused_at = NULL,
		     last_resume_at = ?,
		     updated_at = ?
		 WHERE id = ? AND end_at IS NULL AND paused = 1`
	args := []any{nowStr, FormatTime(now.UTC()), id}
	if teamID > 0 {
		q += ` AND team_id = ?`
		args = append(args, teamID)
	}
	res, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return model.Session{}, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return model.Session{}, err
	} else if n != 1 {
		return model.Session{}, ErrNotFound
	}
	updated, err := getSessionTx(ctx, tx, teamID, id)
	if err != nil {
		return model.Session{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Session{}, err
	}
	return updated, nil
}

// ReopenSession undoes a stop: clears end_at and, for a session that was
// running, resumes it from the stop moment so the undo leaves no gap.
// accumulated_seconds already holds the time folded in at stop.
func (d *DB) ReopenSession(ctx context.Context, request appmodel.TimerReopenRequest) (model.Session, error) {
	teamID, id, reopenedAt := request.TeamID, request.SessionID, request.At
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.Session{}, err
	}
	defer tx.Rollback()
	// Resolve and lock the session owner before locking the session row. This
	// uses the same owner-first order as StartSession and member removal.
	ownerQuery := `SELECT user_id FROM sessions WHERE id = ?`
	ownerArgs := []any{id}
	var scope string
	scope, ownerArgs = scopeSQL(ctx, "user_id", ownerArgs)
	ownerQuery += scope
	if teamID > 0 {
		ownerQuery += ` AND team_id = ?`
		ownerArgs = append(ownerArgs, teamID)
	}
	var ownerValue sql.NullInt64
	if err := tx.QueryRowContext(ctx, ownerQuery, ownerArgs...).Scan(&ownerValue); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Session{}, ErrNotFound
		}
		return model.Session{}, err
	}
	ownerID := ownerValue.Int64
	if ownerID > 0 {
		var lockedOwner int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id = ? FOR UPDATE`, ownerID).Scan(&lockedOwner); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return model.Session{}, ErrNotFound
			}
			return model.Session{}, err
		}
	}
	if err := requireTeamMembership(ctx, tx, teamID); err != nil {
		return model.Session{}, err
	}
	s, err := getSessionTx(ctx, tx, teamID, id)
	if err != nil {
		return model.Session{}, err
	}
	if request.ExpectedEndAt.IsZero() || s.EndAt == nil || !s.EndAt.Equal(request.ExpectedEndAt) {
		return model.Session{}, model.ErrSessionReopenExpired
	}
	if teamID > 0 {
		var memberID int64
		if err := tx.QueryRowContext(ctx,
			`SELECT user_id FROM memberships WHERE team_id = ? AND user_id = ? FOR SHARE`, teamID, ownerID).Scan(&memberID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return model.Session{}, ErrNotFound
			}
			return model.Session{}, err
		}
	}
	var activeID int64
	activeQuery := `SELECT id FROM sessions WHERE activity_id = ? AND end_at IS NULL AND user_id = ? AND id <> ?`
	activeArgs := []any{s.ActivityID, ownerID, id}
	if teamID > 0 {
		activeQuery += ` AND team_id = ?`
		activeArgs = append(activeArgs, teamID)
	}
	err = tx.QueryRowContext(ctx, activeQuery+` LIMIT 1 FOR UPDATE`, activeArgs...).Scan(&activeID)
	if err == nil {
		return model.Session{}, model.ErrActiveSessionExists
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return model.Session{}, err
	}
	q := `UPDATE sessions SET end_at = NULL, updated_at = ?`
	args := []any{FormatTime(reopenedAt.UTC())}
	if !s.Paused {
		q += `, last_resume_at = ?`
		args = append(args, FormatTime(*s.EndAt))
	}
	q += ` WHERE id = ? AND end_at IS NOT NULL`
	args = append(args, id)
	if teamID > 0 {
		q += ` AND team_id = ?`
		args = append(args, teamID)
	}
	res, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return model.Session{}, err
	}
	if affected, err := res.RowsAffected(); err != nil {
		return model.Session{}, err
	} else if affected != 1 {
		return model.Session{}, ErrNotFound
	}
	updated, err := getSessionTx(ctx, tx, teamID, id)
	if err != nil {
		return model.Session{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Session{}, err
	}
	return updated, nil
}
