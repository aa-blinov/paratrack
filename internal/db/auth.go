package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/teamslug"
)

// CreateAccount inserts a user, personal workspace and owner membership in
// one transaction. A registered account is usable only when its workspace is
// present, so the three rows form one persistence invariant.
func (d *DB) CreateAccount(ctx context.Context, request appmodel.AccountCreateRequest) (userID, teamID int64, err error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("begin account creation: %w", err)
	}
	defer tx.Rollback()

	request.Name = strings.TrimSpace(request.Name)
	request.TeamName = strings.TrimSpace(request.TeamName)
	stamp := FormatTime(d.currentTime().UTC())
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO users (email, password_hash, name, created_at, updated_at) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		request.Email, request.PasswordHash, request.Name, stamp, stamp,
	).Scan(&userID); err != nil {
		return 0, 0, fmt.Errorf("insert user: %w", err)
	}

	slug := teamslug.PersonalSlug(userID, request.Name)
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO teams (slug, name, owner_id, created_at) VALUES (?, ?, ?, ?) RETURNING id`,
		slug, request.TeamName, userID, stamp,
	).Scan(&teamID); err != nil {
		return 0, 0, fmt.Errorf("create personal team: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'owner', ?)`,
		teamID, userID, stamp,
	); err != nil {
		return 0, 0, fmt.Errorf("create personal team membership: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit account creation: %w", err)
	}
	return userID, teamID, nil
}

func (d *DB) FindUserByEmail(ctx context.Context, email string) (model.User, error) {
	return scanAuthUser(d.sql.QueryRowContext(ctx,
		`SELECT id, email, password_hash, name, created_at, updated_at FROM users WHERE email = ?`,
		strings.ToLower(strings.TrimSpace(email)),
	))
}

func (d *DB) FindUserByID(ctx context.Context, id int64) (model.User, error) {
	return scanAuthUser(d.sql.QueryRowContext(ctx,
		`SELECT id, email, password_hash, name, created_at, updated_at FROM users WHERE id = ?`, id,
	))
}

func (d *DB) FindUserIdentitiesByIDs(ctx context.Context, ids []int64) (map[int64]appmodel.UserIdentity, error) {
	identities := make(map[int64]appmodel.UserIdentity, len(ids))
	if len(ids) == 0 {
		return identities, nil
	}
	rows, err := d.sql.QueryContext(ctx, `SELECT id, email, name FROM users WHERE id = ANY(?)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var identity appmodel.UserIdentity
		if err := rows.Scan(&identity.ID, &identity.Email, &identity.Name); err != nil {
			return nil, err
		}
		identities[identity.ID] = identity
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return identities, nil
}

func (d *DB) UpdateUserName(ctx context.Context, request appmodel.ProfileNameRequest) error {
	if request.UserID <= 0 {
		return model.ErrNotFound
	}
	if request.CallerID <= 0 || request.CallerID != actorID(ctx) || request.UserID != request.CallerID {
		return model.ErrForbidden
	}
	return execRequireRows(ctx, d.sql, `UPDATE users SET name = ?, updated_at = ? WHERE id = ?`, request.Name, FormatTime(d.currentTime().UTC()), request.UserID)
}

func (d *DB) UpdateUserPasswordIfHashMatches(ctx context.Context, request appmodel.PasswordHashUpdateRequest) (bool, error) {
	if request.CallerID <= 0 || request.CallerID != actorID(ctx) || request.UserID != request.CallerID {
		return false, model.ErrForbidden
	}
	result, err := d.sql.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ? AND password_hash = ?`,
		request.PasswordHash, FormatTime(d.currentTime().UTC()), request.UserID, request.ExpectedHash,
	)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

func (d *DB) CreateAuthSession(ctx context.Context, request appmodel.AuthSessionCreateRequest) (model.AuthSession, error) {
	if _, err := d.sql.ExecContext(ctx,
		`INSERT INTO auth_sessions (token, user_id, created_at, expires_at, last_seen_at) VALUES (?, ?, ?, ?, ?)`,
		request.Token, request.UserID, FormatTime(request.CreatedAt), FormatTime(request.ExpiresAt), FormatTime(request.CreatedAt),
	); err != nil {
		return model.AuthSession{}, err
	}
	return model.AuthSession{Token: request.Token, UserID: request.UserID, CreatedAt: request.CreatedAt, ExpiresAt: request.ExpiresAt, LastSeenAt: request.CreatedAt}, nil
}

func (d *DB) FindAuthSession(ctx context.Context, token string) (model.AuthSession, model.User, error) {
	var session model.AuthSession
	var user model.User
	var created, expires, lastSeen, userCreated, userUpdated string
	err := d.sql.QueryRowContext(ctx, `
		SELECT s.token, s.user_id, s.created_at, s.expires_at, s.last_seen_at,
		       u.id, u.email, u.password_hash, u.name, u.created_at, u.updated_at
		FROM auth_sessions s JOIN users u ON u.id = s.user_id WHERE s.token = ?`, token,
	).Scan(&session.Token, &session.UserID, &created, &expires, &lastSeen,
		&user.ID, &user.Email, &user.PasswordHash, &user.Name, &userCreated, &userUpdated)
	if errors.Is(err, sql.ErrNoRows) {
		return model.AuthSession{}, model.User{}, ErrNotFound
	}
	if err != nil {
		return model.AuthSession{}, model.User{}, err
	}
	if session.CreatedAt, err = ScanTime(created); err != nil {
		return model.AuthSession{}, model.User{}, fmt.Errorf("parse auth session creation time: %w", err)
	}
	if session.ExpiresAt, err = ScanTime(expires); err != nil {
		return model.AuthSession{}, model.User{}, fmt.Errorf("parse auth session expiry: %w", err)
	}
	if session.LastSeenAt, err = ScanTime(lastSeen); err != nil {
		return model.AuthSession{}, model.User{}, fmt.Errorf("parse auth session last-seen time: %w", err)
	}
	if user.CreatedAt, err = ScanTime(userCreated); err != nil {
		return model.AuthSession{}, model.User{}, fmt.Errorf("parse user creation time: %w", err)
	}
	if user.UpdatedAt, err = ScanTime(userUpdated); err != nil {
		return model.AuthSession{}, model.User{}, fmt.Errorf("parse user update time: %w", err)
	}
	return session, user, nil
}

func (d *DB) TouchAuthSession(ctx context.Context, request appmodel.AuthSessionTouchRequest) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE auth_sessions SET last_seen_at = ? WHERE token = ?`, FormatTime(request.At), request.Token)
	return err
}

func (d *DB) DeleteAuthSession(ctx context.Context, request appmodel.AuthSessionDeleteRequest) error {
	_, err := d.sql.ExecContext(ctx, `DELETE FROM auth_sessions WHERE token = ?`, request.Token)
	return err
}

func (d *DB) DeleteAuthSessionsByUser(ctx context.Context, request appmodel.AuthSessionsDeleteByUserRequest) error {
	_, err := d.sql.ExecContext(ctx, `DELETE FROM auth_sessions WHERE user_id = ?`, request.UserID)
	return err
}

func (d *DB) PurgeExpiredAuthSessions(ctx context.Context, request appmodel.AuthSessionsPurgeExpiredRequest) (int64, error) {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM auth_sessions WHERE expires_at <= ?`, FormatTime(request.Before))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (d *DB) CreatePasswordReset(ctx context.Context, request appmodel.PasswordResetCreateRequest) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var lockedUserID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id = ? FOR UPDATE`, request.UserID).Scan(&lockedUserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE password_reset_tokens SET used_at = ? WHERE user_id = ? AND used_at IS NULL`, FormatTime(request.CreatedAt), request.UserID,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO password_reset_tokens (token, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		request.TokenHash, request.UserID, FormatTime(request.CreatedAt), FormatTime(request.ExpiresAt),
	); err != nil {
		return err
	}
	return tx.Commit()
}

// ConsumePasswordReset atomically validates and consumes a reset token,
// changes the password, and revokes all existing browser sessions.
func (d *DB) ConsumePasswordReset(ctx context.Context, request appmodel.PasswordResetConsumeRequest) (model.User, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.User{}, err
	}
	defer tx.Rollback()

	// Keep the same lock order as CreatePasswordReset (user, then token).
	// Locking both sides of a join here could invert that order and deadlock
	// against a concurrent request that replaces outstanding reset tokens.
	var storedToken string
	var userID int64
	if err := tx.QueryRowContext(ctx,
		`SELECT token, user_id FROM password_reset_tokens
		 WHERE token = ? OR token = ?
		 ORDER BY CASE WHEN token = ? THEN 0 ELSE 1 END LIMIT 1`, request.TokenHash, request.LegacyToken, request.TokenHash,
	).Scan(&storedToken, &userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.User{}, ErrNotFound
		}
		return model.User{}, err
	}

	var user model.User
	var created, updated string
	err = tx.QueryRowContext(ctx,
		`SELECT id, email, password_hash, name, created_at, updated_at FROM users WHERE id = ? FOR UPDATE`, userID,
	).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Name, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return model.User{}, ErrNotFound
	}
	if err != nil {
		return model.User{}, err
	}

	var expires, used sql.NullString
	err = tx.QueryRowContext(ctx,
		`SELECT expires_at, used_at FROM password_reset_tokens WHERE token = ? AND user_id = ? FOR UPDATE`, storedToken, userID,
	).Scan(&expires, &used)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && used.Valid) {
		return model.User{}, ErrNotFound
	}
	if err != nil {
		return model.User{}, err
	}
	if !expires.Valid {
		return model.User{}, ErrNotFound
	}
	expiresAt, err := ScanTime(expires.String)
	if err != nil {
		return model.User{}, fmt.Errorf("parse password reset expiry: %w", err)
	}
	if expiresAt.IsZero() || !request.At.Before(expiresAt) {
		return model.User{}, ErrNotFound
	}
	if err := execRequireRows(ctx, tx,
		`UPDATE password_reset_tokens SET token = ?, used_at = ? WHERE token = ? AND used_at IS NULL`,
		request.TokenHash, FormatTime(request.At), storedToken,
	); err != nil {
		return model.User{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`, request.PasswordHash, FormatTime(request.At), user.ID); err != nil {
		return model.User{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_sessions WHERE user_id = ?`, user.ID); err != nil {
		return model.User{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.User{}, err
	}
	user.PasswordHash = request.PasswordHash
	user.UpdatedAt = request.At
	user.CreatedAt, err = ScanTime(created)
	if err != nil {
		return model.User{}, fmt.Errorf("parse user creation time: %w", err)
	}
	return user, nil
}

func scanAuthUser(row interface{ Scan(...any) error }) (model.User, error) {
	var user model.User
	var created, updated string
	if err := row.Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Name, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.User{}, ErrNotFound
		}
		return model.User{}, err
	}
	createdAt, err := ScanTime(created)
	if err != nil {
		return model.User{}, fmt.Errorf("parse user creation time: %w", err)
	}
	user.CreatedAt = createdAt
	user.UpdatedAt, err = ScanTime(updated)
	if err != nil {
		return model.User{}, fmt.Errorf("parse user update time: %w", err)
	}
	return user, nil
}
