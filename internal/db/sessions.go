package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/webhookport"
)

var ErrSessionNotActive = model.ErrSessionNotActive

// SessionUpdate is kept as an adapter alias for callers that used the db type.
type SessionUpdate = appmodel.SessionUpdate

// createLegacySession inserts an unscoped active session for legacy data.
// Workspace sessions must use StartSession, which enforces membership and
// the one-active-session rule in the same transaction.
func (d *DB) createLegacySession(ctx context.Context, request legacySessionCreateRequest) (model.Session, error) {
	const teamID int64 = 0
	activityID, startAt, note := request.ActivityID, request.At, request.Note
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.Session{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockActivityForSession(ctx, tx, teamID, activityID); err != nil {
		return model.Session{}, err
	}
	startStr := FormatTime(startAt)
	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO sessions (activity_id, team_id, start_at, note, paused, accumulated_seconds, last_resume_at, user_id)
		 VALUES (?, ?, ?, ?, 0, 0, ?, ?) RETURNING id`,
		activityID, nullableInt64(teamID), startStr, nullableString(note), startStr, actorOf(ctx),
	).Scan(&id)
	if err != nil {
		return model.Session{}, err
	}
	session, err := getSessionTx(ctx, tx, teamID, id)
	if err != nil {
		return model.Session{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Session{}, err
	}
	return session, nil
}

// StartSession atomically enforces one open session per activity. It locks the
// actor while composing start/focus operations, then the activity to protect
// the per-activity uniqueness rule.
func (d *DB) StartSession(ctx context.Context, request appmodel.TimerStartRequest) (model.Session, error) {
	teamID, activityID, startAt, note := request.TeamID, request.ActivityID, request.At, request.Note
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.Session{}, err
	}
	defer tx.Rollback()
	if err := lockSessionOwner(ctx, tx, teamID); err != nil {
		return model.Session{}, err
	}
	if err := requireTeamMembership(ctx, tx, teamID); err != nil {
		return model.Session{}, err
	}

	if err := lockActivityForSession(ctx, tx, teamID, activityID); err != nil {
		return model.Session{}, err
	}

	activeQuery := `SELECT s.id FROM sessions s WHERE s.activity_id = ? AND s.end_at IS NULL`
	activeArgs := []any{activityID}
	if teamID > 0 {
		activeQuery += ` AND s.team_id = ?`
		activeArgs = append(activeArgs, teamID)
	}
	if actor := actorID(ctx); actor > 0 {
		activeQuery += ` AND s.user_id = ?`
		activeArgs = append(activeArgs, actor)
	} else {
		scope, args := scopeSQL(ctx, `s.user_id`, activeArgs)
		activeQuery += scope
		activeArgs = args
	}
	activeQuery += ` LIMIT 1`
	var existingID int64
	err = tx.QueryRowContext(ctx, activeQuery, activeArgs...).Scan(&existingID)
	if err == nil {
		return model.Session{}, model.ErrActiveSessionExists
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return model.Session{}, err
	}

	startStr := FormatTime(startAt)
	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO sessions (activity_id, team_id, start_at, note, paused, accumulated_seconds, last_resume_at, user_id)
		 VALUES (?, ?, ?, ?, 0, 0, ?, ?) RETURNING id`,
		activityID, nullableInt64(teamID), startStr, nullableString(note), startStr, actorOf(ctx),
	).Scan(&id)
	if err != nil {
		return model.Session{}, err
	}
	var activityName string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM activities WHERE id = ?`, activityID).Scan(&activityName); err != nil {
		return model.Session{}, err
	}
	if err := d.recordWebhookEventTx(ctx, tx, teamID, webhookport.SessionStartedEvent{SessionID: id, Activity: activityName}, startAt); err != nil {
		return model.Session{}, err
	}
	session, err := getSessionTx(ctx, tx, teamID, id)
	if err != nil {
		return model.Session{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Session{}, err
	}
	return session, nil
}

func lockActivityForSession(ctx context.Context, tx *Tx, teamID, activityID int64) error {
	query := `SELECT id FROM activities WHERE id = ?`
	args := []any{activityID}
	if teamID > 0 {
		query += ` AND team_id = ?`
		args = append(args, teamID)
	} else {
		query += ` AND team_id IS NULL`
	}
	var lockedID int64
	if err := tx.QueryRowContext(ctx, query+` FOR UPDATE`, args...).Scan(&lockedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// UpdateSessionFields applies a partial edit within the request's workspace
// and user scope. It keeps dynamic SQL construction inside the storage layer.
func (d *DB) UpdateSessionFields(ctx context.Context, request appmodel.SessionUpdateRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.SessionID <= 0 {
		return ErrNotFound
	}
	teamID, actorID, id, update := request.TeamID, request.CallerID, request.SessionID, request.Update
	if update.AccumulatedSeconds != nil {
		if *update.AccumulatedSeconds < 0 {
			return appmodel.ErrInvalidSessionLength
		}
		if int64(*update.AccumulatedSeconds) > model.MaxSessionDurationSeconds {
			return model.ErrSessionDurationOverflow
		}
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	ownerID, role, err := lockTeamRole(ctx, tx, teamID, actorID)
	if err != nil {
		return err
	}
	if actorID == ownerID && role != model.TeamRoleOwner {
		return model.ErrForbidden
	}
	editScopeUserID := int64(0)
	if !role.CanManage() {
		editScopeUserID = actorID
	}
	lockedSession, err := lockSessionForEdit(ctx, tx, teamID, id, editScopeUserID)
	if err != nil {
		return err
	}
	sets := make([]string, 0, 5)
	args := make([]any, 0, 7)
	if update.StartAt != nil {
		sets = append(sets, `start_at = ?`)
		args = append(args, FormatTime(*update.StartAt))
	}
	if update.EndAt != nil {
		sets = append(sets, `end_at = ?`)
		args = append(args, FormatTime(*update.EndAt))
	}
	if update.AccumulatedSeconds != nil {
		sets = append(sets, `accumulated_seconds = ?`)
		args = append(args, *update.AccumulatedSeconds)
	}
	sets = append(sets, `note = ?`, `updated_at = ?`)
	args = append(args, nullableString(update.Note), FormatTime(update.UpdatedAt))
	args = append(args, id)
	query := `UPDATE sessions SET ` + strings.Join(sets, `, `) + ` WHERE id = ?`
	if teamID > 0 {
		query += ` AND team_id = ?`
		args = append(args, teamID)
	}
	if !role.CanManage() {
		query += ` AND user_id = ?`
		args = append(args, actorID)
	}
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return ErrNotFound
	}
	if lockedSession.wasOpen && update.EndAt != nil {
		startAt, err := ScanTime(lockedSession.startAt)
		if err != nil {
			return fmt.Errorf("parse session start for stop event: %w", err)
		}
		if update.StartAt != nil {
			startAt = *update.StartAt
		}
		if err := d.recordWebhookEventTx(ctx, tx, teamID, webhookport.SessionStoppedEvent{
			SessionID: id, ActivityID: lockedSession.activityID,
			Start: startAt.UTC().Format(time.RFC3339),
		}, update.UpdatedAt); err != nil {
			return fmt.Errorf("record edited session stop: %w", err)
		}
	}
	return tx.Commit()
}

type sessionEditState struct {
	invoiceID  int64
	activityID int64
	startAt    string
	wasOpen    bool
}

// lockSessionForEdit serializes a session edit with invoice status changes.
// Sent and paid invoices freeze their linked sessions; drafts remain editable.
func lockSessionForEdit(ctx context.Context, tx *Tx, teamID, id, scopeUserID int64) (sessionEditState, error) {
	query := `SELECT COALESCE(invoice_id, 0), activity_id, start_at, end_at FROM sessions WHERE id = ?`
	args := []any{id}
	if teamID > 0 {
		query += ` AND team_id = ?`
		args = append(args, teamID)
	}
	if scopeUserID > 0 {
		query += ` AND user_id = ?`
		args = append(args, scopeUserID)
	}
	query += ` FOR UPDATE`
	var state sessionEditState
	var startAt string
	var endAt sql.NullString
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&state.invoiceID, &state.activityID, &startAt, &endAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sessionEditState{}, ErrNotFound
		}
		return sessionEditState{}, err
	}
	state.startAt = startAt
	state.wasOpen = !endAt.Valid
	if state.invoiceID == 0 {
		return state, nil
	}
	var number, status string
	if err := tx.QueryRowContext(ctx,
		`SELECT number, status FROM invoices WHERE id = ? FOR UPDATE`, state.invoiceID).Scan(&number, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return state, nil
		}
		return sessionEditState{}, err
	}
	if status == "sent" || status == "paid" {
		return sessionEditState{}, &model.SessionInvoiceLockError{InvoiceNumber: number}
	}
	return state, nil
}

// CreateClosedSession inserts a finished session in one go. Used by the
// `add` command for back-filling past intervals.
func (d *DB) CreateClosedSession(ctx context.Context, request appmodel.TimerAddRequest) (model.Session, error) {
	teamID, activityID, startAt, endAt, note := request.TeamID, request.ActivityID, request.Start, request.End, request.Note
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.Session{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockSessionOwner(ctx, tx, teamID); err != nil {
		return model.Session{}, err
	}
	if err := requireTeamMembership(ctx, tx, teamID); err != nil {
		return model.Session{}, err
	}
	if err := lockActivityForSession(ctx, tx, teamID, activityID); err != nil {
		return model.Session{}, err
	}
	id, err := insertClosedSessionTx(ctx, tx, teamID, activityID, startAt, endAt, note)
	if err != nil {
		return model.Session{}, err
	}
	session, err := getSessionTx(ctx, tx, teamID, id)
	if err != nil {
		return model.Session{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Session{}, err
	}
	return session, nil
}

func insertClosedSessionTx(ctx context.Context, tx *Tx, teamID, activityID int64, startAt, endAt time.Time, note string) (int64, error) {
	duration := endAt.Sub(startAt)
	if duration <= 0 {
		return 0, appmodel.ErrInvalidSessionPeriod
	}
	if duration > time.Duration(model.MaxSessionDurationSeconds)*time.Second {
		return 0, model.ErrSessionDurationOverflow
	}
	startStr := FormatTime(startAt)
	endStr := FormatTime(endAt)
	var id int64
	err := tx.QueryRowContext(ctx,
		`INSERT INTO sessions (activity_id, team_id, start_at, end_at, note, paused, accumulated_seconds, last_resume_at, user_id)
		 VALUES (?, ?, ?, ?, ?, 0, ?, NULL, ?) RETURNING id`,
		activityID, nullableInt64(teamID), startStr, endStr, nullableString(note),
		max(0, int(endAt.Sub(startAt).Seconds())), actorOf(ctx), // a closed session carries its tracked total
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func getSessionTx(ctx context.Context, tx *Tx, teamID, id int64) (model.Session, error) {
	query := sessionSelect + ` WHERE s.id = ?`
	args := []any{id}
	var scope string
	scope, args = scopeSQL(ctx, "s.user_id", args)
	query += scope
	if teamID > 0 {
		query += ` AND s.team_id = ?`
		args = append(args, teamID)
	}
	query += ` FOR UPDATE OF s`
	session, err := scanSession(tx.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Session{}, ErrNotFound
	}
	return session, err
}

// DeleteSession removes a session by id (FK cascades handle session_tags).
// teamID > 0 restricts the delete to that workspace.
func (d *DB) DeleteSession(ctx context.Context, request appmodel.SessionDeleteRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.SessionID <= 0 {
		return ErrNotFound
	}
	teamID, actorID, id := request.TeamID, request.CallerID, request.SessionID
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	ownerID, role, err := lockTeamRole(ctx, tx, teamID, actorID)
	if err != nil {
		return err
	}
	if actorID == ownerID && role != model.TeamRoleOwner {
		return model.ErrForbidden
	}
	editScopeUserID := int64(0)
	if !role.CanManage() {
		editScopeUserID = actorID
	}
	if _, err := lockSessionForEdit(ctx, tx, teamID, id, editScopeUserID); err != nil {
		return err
	}
	q := `DELETE FROM sessions WHERE id = ?`
	args := []any{id}
	if teamID > 0 {
		q += ` AND team_id = ?`
		args = append(args, teamID)
	}
	if !role.CanManage() {
		q += ` AND user_id = ?`
		args = append(args, actorID)
	}
	res, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
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
