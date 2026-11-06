package db

import (
	"context"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// Idempotency keys make a replayed write safe. The offline queue sends the same
// key on every try of one queued write. The first attempt claims the key, and a
// finished attempt keeps its status, so a replay returns that status instead of
// writing a second time. A claim whose attempt never finished (the process died
// mid-write) is taken over once it is older than idempotencyStaleAfter.
const idempotencyStaleAfter = 2 * time.Minute

// ClaimIdempotencyKey returns the status of a finished attempt when the key is
// already done. Otherwise it claims the key for the caller and returns done false.
// It returns model.ErrRequestInProgress while a fresh claim by another attempt
// is still open.
func (d *DB) ClaimIdempotencyKey(ctx context.Context, request appmodel.IdempotencyRequest) (bool, int, error) {
	userID, key, now := request.UserID, request.Key, request.Now
	nowStr := FormatTime(now.UTC())
	res, err := d.sql.ExecContext(ctx,
		`INSERT INTO idempotency_keys (user_id, request_key, status, created_at) VALUES (?, ?, 0, ?)
		 ON CONFLICT (user_id, request_key) DO NOTHING`, userID, key, nowStr)
	if err != nil {
		return false, 0, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return false, 0, err
	} else if n == 1 {
		return false, 0, nil
	}

	var status int
	var claimedRaw string
	if err := d.sql.QueryRowContext(ctx,
		`SELECT status, created_at FROM idempotency_keys WHERE user_id = ? AND request_key = ?`,
		userID, key).Scan(&status, &claimedRaw); err != nil {
		return false, 0, err
	}
	if status > 0 {
		return true, status, nil
	}
	claimed, err := ParseTime(claimedRaw)
	if err != nil {
		return false, 0, err
	}
	if now.Sub(claimed) < idempotencyStaleAfter {
		return false, 0, model.ErrRequestInProgress
	}
	// Take over only the stale claim that was read, so two retries cannot both win.
	res, err = d.sql.ExecContext(ctx,
		`UPDATE idempotency_keys SET created_at = ?
		  WHERE user_id = ? AND request_key = ? AND status = 0 AND created_at = ?`,
		nowStr, userID, key, claimedRaw)
	if err != nil {
		return false, 0, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return false, 0, err
	} else if n != 1 {
		return false, 0, model.ErrRequestInProgress
	}
	return false, 0, nil
}

// FinishIdempotencyKey records the status a finished attempt returned.
func (d *DB) FinishIdempotencyKey(ctx context.Context, request appmodel.IdempotencyRequest) error {
	_, err := d.sql.ExecContext(ctx,
		`UPDATE idempotency_keys SET status = ? WHERE user_id = ? AND request_key = ?`,
		request.Status, request.UserID, request.Key)
	return err
}

// ReleaseIdempotencyKey drops a claim whose attempt failed, so the next try runs.
func (d *DB) ReleaseIdempotencyKey(ctx context.Context, request appmodel.IdempotencyRequest) error {
	_, err := d.sql.ExecContext(ctx,
		`DELETE FROM idempotency_keys WHERE user_id = ? AND request_key = ? AND status = 0`,
		request.UserID, request.Key)
	return err
}
