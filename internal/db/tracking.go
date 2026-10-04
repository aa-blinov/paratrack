package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/webhookport"
)

type openSession struct {
	id, activityID, accumulated int64
	startAt                     time.Time
	lastResume                  *time.Time
	paused                      bool
}

func addSessionSeconds(accumulated, elapsed int64) (int64, error) {
	if accumulated < 0 || elapsed < 0 {
		return 0, appmodel.ErrInvalidSessionLength
	}
	maxSeconds := model.MaxSessionDurationSeconds
	maxInt := int64(int(^uint(0) >> 1))
	if maxSeconds > maxInt {
		maxSeconds = maxInt
	}
	if accumulated > maxSeconds-elapsed {
		return 0, model.ErrSessionDurationOverflow
	}
	return accumulated + elapsed, nil
}

// lockOpenSessions reads the current actor/workspace's sessions in stable
// order while holding their row locks for a transition transaction.
func lockOpenSessions(ctx context.Context, tx *Tx, teamID, userID int64, runningOnly bool) ([]openSession, error) {
	query := `SELECT id, activity_id, start_at, last_resume_at, paused, accumulated_seconds FROM sessions WHERE end_at IS NULL`
	if runningOnly {
		query += ` AND paused = 0`
	}
	args := make([]any, 0, 3)
	if teamID > 0 {
		query += ` AND team_id = ?`
		args = append(args, teamID)
	}
	if userID > 0 {
		query += ` AND user_id = ?`
		args = append(args, userID)
	} else if actor := actorID(ctx); actor > 0 {
		query += ` AND user_id = ?`
		args = append(args, actor)
	} else {
		scope, scopedArgs := scopeSQL(ctx, `user_id`, args)
		query += scope
		args = scopedArgs
	}
	query += ` ORDER BY id FOR UPDATE`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []openSession
	for rows.Next() {
		var session openSession
		var start string
		var lastResume sql.NullString
		var paused int64
		if err := rows.Scan(&session.id, &session.activityID, &start, &lastResume, &paused, &session.accumulated); err != nil {
			_ = rows.Close()
			return nil, err
		}
		session.startAt, err = ScanTime(start)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		if lastResume.Valid {
			parsed, err := ScanTime(lastResume.String)
			if err != nil {
				_ = rows.Close()
				return nil, err
			}
			session.lastResume = &parsed
		}
		session.paused = paused != 0
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return sessions, nil
}

// FocusActivity applies the focus transition as one transaction: all other
// running sessions are paused and the chosen activity is resumed or started.
func (d *DB) FocusActivity(ctx context.Context, request appmodel.TimerFocusRequest) (appmodel.FocusResult, error) {
	teamID, activityID, now := request.TeamID, request.ActivityID, request.At
	result := appmodel.FocusResult{}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err := lockSessionOwner(ctx, tx, teamID); err != nil {
		return result, err
	}
	if err := requireTeamMembership(ctx, tx, teamID); err != nil {
		return result, err
	}

	activityQuery := `SELECT id, name FROM activities WHERE id = ?`
	activityArgs := []any{activityID}
	if teamID > 0 {
		activityQuery += ` AND team_id = ?`
		activityArgs = append(activityArgs, teamID)
	}
	var foundActivity int64
	var activityName string
	if err := tx.QueryRowContext(ctx, activityQuery+` FOR UPDATE`, activityArgs...).Scan(&foundActivity, &activityName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return result, ErrNotFound
		}
		return result, err
	}

	active, err := lockOpenSessions(ctx, tx, teamID, 0, false)
	if err != nil {
		return result, err
	}

	targetExists := false
	nowValue := FormatTime(now)
	updatedValue := FormatTime(now.UTC())
	for _, session := range active {
		if session.activityID == activityID {
			targetExists = true
			if session.paused {
				if _, err := tx.ExecContext(ctx,
					`UPDATE sessions SET paused = 0, paused_at = NULL, last_resume_at = ?, updated_at = ? WHERE id = ?`,
					nowValue, updatedValue, session.id); err != nil {
					return result, err
				}
				result.Resumed++
			}
			continue
		}
		if session.paused {
			continue
		}
		anchor := session.lastResume
		if anchor == nil {
			anchor = &session.startAt
		}
		elapsed := int64(now.Sub(*anchor).Seconds())
		if elapsed < 0 {
			elapsed = 0
		}
		accumulated, err := addSessionSeconds(session.accumulated, elapsed)
		if err != nil {
			return result, fmt.Errorf("pause session %d: %w", session.id, err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE sessions SET paused = 1, paused_at = ?, accumulated_seconds = ?, last_resume_at = NULL, updated_at = ? WHERE id = ?`,
			nowValue, accumulated, updatedValue, session.id); err != nil {
			return result, err
		}
		result.Paused++
	}

	if !targetExists {
		if err := tx.QueryRowContext(ctx,
			`INSERT INTO sessions (activity_id, team_id, start_at, note, paused, accumulated_seconds, last_resume_at, user_id)
			 VALUES (?, ?, ?, NULL, 0, 0, ?, ?) RETURNING id`,
			activityID, nullableInt64(teamID), nowValue, nowValue, actorOf(ctx)).Scan(&result.StartedSessionID); err != nil {
			return result, err
		}
		result.Started = true
		if err := d.recordWebhookEventTx(ctx, tx, teamID, webhookport.SessionStartedEvent{SessionID: result.StartedSessionID, Activity: activityName}, now); err != nil {
			return result, err
		}
	}
	if err := tx.Commit(); err != nil {
		return appmodel.FocusResult{}, err
	}
	return result, nil
}

// StopActiveSessions closes every open session in the current actor/workspace
// scope atomically. Elapsed time since the last resume is folded into the
// stored total; paused gaps remain excluded.
func (d *DB) StopActiveSessions(ctx context.Context, request appmodel.TimerStopAllRequest) ([]model.Session, error) {
	teamID, endAt := request.TeamID, request.At
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockSessionOwner(ctx, tx, teamID); err != nil {
		return nil, err
	}
	if err := requireTeamMembership(ctx, tx, teamID); err != nil {
		return nil, err
	}

	sessions, err := lockOpenSessions(ctx, tx, teamID, 0, false)
	if err != nil {
		return nil, err
	}

	endAt = endAt.UTC()
	endValue := FormatTime(endAt)
	updatedValue := FormatTime(endAt)
	stopped := make([]model.Session, 0, len(sessions))
	for _, session := range sessions {
		accumulated, err := accumulatedAtEnd(session.accumulated, session.startAt, session.lastResume, session.paused, endAt)
		if err != nil {
			return nil, fmt.Errorf("stop session %d: %w", session.id, err)
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE sessions SET end_at = ?, accumulated_seconds = ?, updated_at = ? WHERE id = ? AND end_at IS NULL`,
			endValue, accumulated, updatedValue, session.id)
		if err != nil {
			return nil, err
		}
		if affected, err := res.RowsAffected(); err != nil {
			return nil, err
		} else if affected != 1 {
			return nil, ErrNotFound
		}
		end := endAt
		stopped = append(stopped, model.Session{
			ID: session.id, ActivityID: session.activityID, TeamID: teamID,
			UserID: actorID(ctx), StartAt: session.startAt, EndAt: &end,
			Paused: session.paused, AccumulatedSeconds: int(accumulated),
			LastResumeAt: session.lastResume, UpdatedAt: endAt,
		})
		if err := d.recordWebhookEventTx(ctx, tx, teamID, webhookport.SessionStoppedEvent{SessionID: session.id, ActivityID: session.activityID, Start: session.startAt.UTC().Format(time.RFC3339)}, endAt); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return stopped, nil
}

// PauseActiveSessions pauses every running session in the current
// actor/workspace scope in one transaction.
func (d *DB) PauseActiveSessions(ctx context.Context, request appmodel.TimerStopAllRequest) ([]int64, error) {
	teamID, pausedAt := request.TeamID, request.At
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockSessionOwner(ctx, tx, teamID); err != nil {
		return nil, err
	}
	if err := requireTeamMembership(ctx, tx, teamID); err != nil {
		return nil, err
	}
	sessions, err := lockOpenSessions(ctx, tx, teamID, 0, true)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(sessions))
	for _, session := range sessions {
		anchor := session.lastResume
		if anchor == nil {
			anchor = &session.startAt
		}
		elapsed := int64(pausedAt.Sub(*anchor).Seconds())
		if elapsed < 0 {
			elapsed = 0
		}
		accumulated, err := addSessionSeconds(session.accumulated, elapsed)
		if err != nil {
			return nil, fmt.Errorf("pause session %d: %w", session.id, err)
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE sessions SET paused = 1, paused_at = ?, accumulated_seconds = ?, last_resume_at = NULL, updated_at = ?
			 WHERE id = ? AND end_at IS NULL AND paused = 0`,
			FormatTime(pausedAt), accumulated, FormatTime(pausedAt.UTC()), session.id)
		if err != nil {
			return nil, err
		}
		if affected, err := res.RowsAffected(); err != nil {
			return nil, err
		} else if affected != 1 {
			return nil, ErrNotFound
		}
		ids = append(ids, session.id)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return ids, nil
}

func accumulatedAtEnd(accumulated int64, startAt time.Time, lastResume *time.Time, paused bool, endAt time.Time) (int64, error) {
	if paused {
		return addSessionSeconds(accumulated, 0)
	}
	anchor := lastResume
	if anchor == nil {
		anchor = &startAt
	}
	elapsed := int64(endAt.Sub(*anchor).Seconds())
	if elapsed < 0 {
		elapsed = 0
	}
	return addSessionSeconds(accumulated, elapsed)
}

func (d *DB) stopMemberSessionsTx(ctx context.Context, tx *Tx, teamID, userID int64, endAt time.Time) error {
	sessions, err := lockOpenSessions(ctx, tx, teamID, userID, false)
	if err != nil {
		return err
	}
	end := FormatTime(endAt)
	updated := FormatTime(endAt.UTC())
	for _, session := range sessions {
		accumulated, err := accumulatedAtEnd(session.accumulated, session.startAt, session.lastResume, session.paused, endAt)
		if err != nil {
			return fmt.Errorf("stop session %d: %w", session.id, err)
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE sessions SET end_at = ?, accumulated_seconds = ?, updated_at = ? WHERE id = ? AND end_at IS NULL`,
			end, accumulated, updated, session.id)
		if err != nil {
			return err
		}
		if affected, err := res.RowsAffected(); err != nil {
			return err
		} else if affected != 1 {
			return ErrNotFound
		}
		if err := d.recordWebhookEventTx(ctx, tx, teamID, webhookport.SessionStoppedEvent{
			SessionID: session.id, ActivityID: session.activityID,
			Start: session.startAt.UTC().Format(time.RFC3339),
		}, endAt); err != nil {
			return fmt.Errorf("record removed member session stop: %w", err)
		}
	}
	return nil
}
