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

// IssueEmailVerification mints a confirmation link for an account. It refuses a second link
// inside MinInterval and reports false, and it retires the links still unused, so only the
// newest one works.
func (d *DB) IssueEmailVerification(ctx context.Context, request appmodel.EmailVerificationIssueRequest) (bool, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var lockedID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id = ? FOR UPDATE`, request.UserID).Scan(&lockedID); err != nil {
		return false, err
	}
	var latest sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT MAX(created_at) FROM email_verification_tokens WHERE user_id = ?`, request.UserID).Scan(&latest); err != nil {
		return false, err
	}
	if latest.Valid {
		last, err := ParseTime(latest.String)
		if err != nil {
			return false, err
		}
		if request.Now.Sub(last) < request.MinInterval {
			return false, nil
		}
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM email_verification_tokens WHERE user_id = ? AND used_at IS NULL`, request.UserID); err != nil {
		return false, err
	}
	now := FormatTime(request.Now.UTC())
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO email_verification_tokens (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		request.TokenHash, request.UserID, now, FormatTime(request.Now.Add(request.TTL).UTC())); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// ConsumeEmailVerification confirms the address behind a link, once, and returns the account.
// A used, expired or unknown link is ErrNotFound.
func (d *DB) ConsumeEmailVerification(ctx context.Context, request appmodel.EmailVerificationConsumeRequest) (model.User, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.User{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var userID int64
	var expires string
	var used sql.NullString
	err = tx.QueryRowContext(ctx,
		`SELECT user_id, expires_at, used_at FROM email_verification_tokens WHERE token_hash = ? FOR UPDATE`,
		request.TokenHash).Scan(&userID, &expires, &used)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && used.Valid) {
		return model.User{}, ErrNotFound
	}
	if err != nil {
		return model.User{}, err
	}
	expiresAt, err := ParseTime(expires)
	if err != nil {
		return model.User{}, err
	}
	if !request.At.Before(expiresAt) {
		return model.User{}, ErrNotFound
	}
	stamp := FormatTime(request.At.UTC())
	if _, err := tx.ExecContext(ctx,
		`UPDATE email_verification_tokens SET used_at = ? WHERE token_hash = ?`, stamp, request.TokenHash); err != nil {
		return model.User{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET email_unverified = 0 WHERE id = ?`, userID); err != nil {
		return model.User{}, err
	}
	user, err := scanAuthUser(tx.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, userID))
	if err != nil {
		return model.User{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.User{}, err
	}
	return user, nil
}

// TwoFactorState reads the second factor of an account. The secrets are opened here, so the
// workflow above the store only ever sees plain text.
func (d *DB) TwoFactorState(ctx context.Context, userID int64) (appmodel.TwoFactorState, error) {
	var enabled int64
	var secret, pending string
	var lastStep int64
	err := d.sql.QueryRowContext(ctx,
		`SELECT totp_enabled, totp_secret, totp_pending, totp_last_step FROM users WHERE id = ?`, userID,
	).Scan(&enabled, &secret, &pending, &lastStep)
	if errors.Is(err, sql.ErrNoRows) {
		return appmodel.TwoFactorState{}, ErrNotFound
	}
	if err != nil {
		return appmodel.TwoFactorState{}, err
	}
	state := appmodel.TwoFactorState{Enabled: enabled == 1, LastStep: lastStep}
	if state.Secret, err = d.openSecret(secret); err != nil {
		return appmodel.TwoFactorState{}, fmt.Errorf("open two-factor secret: %w", err)
	}
	if state.Pending, err = d.openSecret(pending); err != nil {
		return appmodel.TwoFactorState{}, fmt.Errorf("open pending two-factor secret: %w", err)
	}
	return state, nil
}

// SetTwoFactorPending keeps a secret that a set-up has started but no code has confirmed yet.
func (d *DB) SetTwoFactorPending(ctx context.Context, request appmodel.TwoFactorPendingRequest) error {
	sealed, err := d.sealSecret(request.Secret)
	if err != nil {
		return err
	}
	_, err = d.sql.ExecContext(ctx, `UPDATE users SET totp_pending = ? WHERE id = ?`, sealed, request.UserID)
	return err
}

// EnableTwoFactor turns the second factor on with the confirmed secret, and replaces the
// recovery codes of the account with the new ones.
func (d *DB) EnableTwoFactor(ctx context.Context, request appmodel.TwoFactorEnableRequest) error {
	sealed, err := d.sealSecret(request.Secret)
	if err != nil {
		return err
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET totp_enabled = 1, totp_secret = ?, totp_pending = '', totp_last_step = ? WHERE id = ?`,
		sealed, request.Step, request.UserID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM two_factor_recovery_codes WHERE user_id = ?`, request.UserID); err != nil {
		return err
	}
	for _, hash := range request.CodeHashes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO two_factor_recovery_codes (user_id, code_hash) VALUES (?, ?)`, request.UserID, hash); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RecordTwoFactorStep moves the last used step forward. It reports false when the step was
// used already, so one code cannot sign in twice.
func (d *DB) RecordTwoFactorStep(ctx context.Context, request appmodel.TwoFactorStepRequest) (bool, error) {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE users SET totp_last_step = ? WHERE id = ? AND totp_last_step < ?`, request.Step, request.UserID, request.Step)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// DisableTwoFactor turns the second factor off and drops its secrets, codes and open challenges.
func (d *DB) DisableTwoFactor(ctx context.Context, request appmodel.TwoFactorUserRequest) error {
	userID := request.UserID
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET totp_enabled = 0, totp_secret = '', totp_pending = '', totp_last_step = 0 WHERE id = ?`, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM two_factor_recovery_codes WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM two_factor_challenges WHERE user_id = ?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// CreateTwoFactorChallenge opens the code step of a sign-in, replacing any open one.
func (d *DB) CreateTwoFactorChallenge(ctx context.Context, request appmodel.TwoFactorChallengeCreateRequest) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM two_factor_challenges WHERE user_id = ?`, request.UserID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO two_factor_challenges (token_hash, user_id, created_at, expires_at, attempts) VALUES (?, ?, ?, ?, 0)`,
		request.TokenHash, request.UserID, FormatTime(request.Now.UTC()), FormatTime(request.Now.Add(request.TTL).UTC())); err != nil {
		return err
	}
	return tx.Commit()
}

// TwoFactorChallenge reads an open code step. A missing or expired step is ErrNotFound.
func (d *DB) TwoFactorChallenge(ctx context.Context, tokenHash string, at time.Time) (appmodel.TwoFactorChallenge, error) {
	var challenge appmodel.TwoFactorChallenge
	var expires string
	err := d.sql.QueryRowContext(ctx,
		`SELECT user_id, expires_at, attempts FROM two_factor_challenges WHERE token_hash = ?`, tokenHash,
	).Scan(&challenge.UserID, &expires, &challenge.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return appmodel.TwoFactorChallenge{}, ErrNotFound
	}
	if err != nil {
		return appmodel.TwoFactorChallenge{}, err
	}
	expiresAt, err := ParseTime(expires)
	if err != nil {
		return appmodel.TwoFactorChallenge{}, err
	}
	if !at.Before(expiresAt) {
		return appmodel.TwoFactorChallenge{}, ErrNotFound
	}
	return challenge, nil
}

// RecordChallengeFailure counts a wrong code against the step and returns the count so far.
func (d *DB) RecordChallengeFailure(ctx context.Context, request appmodel.TwoFactorChallengeRequest) (int64, error) {
	var attempts int64
	err := d.sql.QueryRowContext(ctx,
		`UPDATE two_factor_challenges SET attempts = attempts + 1 WHERE token_hash = ? RETURNING attempts`, request.TokenHash,
	).Scan(&attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return attempts, err
}

// DeleteTwoFactorChallenge ends a code step, after it succeeded or ran out of attempts.
func (d *DB) DeleteTwoFactorChallenge(ctx context.Context, request appmodel.TwoFactorChallengeRequest) error {
	_, err := d.sql.ExecContext(ctx, `DELETE FROM two_factor_challenges WHERE token_hash = ?`, request.TokenHash)
	return err
}

// ConsumeRecoveryCode spends one unused recovery code of the account. It reports false when no
// unused code matches.
func (d *DB) ConsumeRecoveryCode(ctx context.Context, request appmodel.RecoveryCodeConsumeRequest) (bool, error) {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE two_factor_recovery_codes SET used_at = ? WHERE user_id = ? AND code_hash = ? AND used_at IS NULL`,
		FormatTime(request.At.UTC()), request.UserID, request.CodeHash)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}
