package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

func (d *DB) CreateTeamInvite(ctx context.Context, request appmodel.TeamInvitePersistenceRequest) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO invites (token, team_id, role, created_by, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		request.Token, request.TeamID, request.Role, request.CallerID, FormatTime(request.CreatedAt), FormatTime(request.ExpiresAt)); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) FindTeamInvite(ctx context.Context, token string) (model.TeamInvite, error) {
	return scanTeamInvite(d.sql.QueryRowContext(ctx, `
		SELECT token, team_id, role, created_by, created_at, expires_at, accepted_at, accepted_by
		FROM invites WHERE token = ?`, token))
}

func (d *DB) ListTeamInvites(ctx context.Context, teamID int64) ([]model.TeamInvite, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT token, team_id, role, created_by, created_at, expires_at, accepted_at, accepted_by
		FROM invites WHERE team_id = ? ORDER BY created_at DESC`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var invites []model.TeamInvite
	for rows.Next() {
		invite, err := scanTeamInvite(rows)
		if err != nil {
			return nil, err
		}
		invites = append(invites, invite)
	}
	return invites, rows.Err()
}

func (d *DB) DeleteTeamInvite(ctx context.Context, request appmodel.TeamInviteRevokeRequest) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return err
	}
	if err := execRequireRows(ctx, tx, `DELETE FROM invites WHERE token = ? AND team_id = ?`, request.Token, request.TeamID); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) AcceptTeamInvite(ctx context.Context, request appmodel.TeamInviteAcceptanceRequest) error {
	if request.TeamID <= 0 || request.UserID <= 0 || request.Token == "" || request.At.IsZero() {
		return model.ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var teamID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM teams WHERE id = ? FOR UPDATE`, request.TeamID).Scan(&teamID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ErrNotFound
		}
		return err
	}
	var expires, role string
	var accepted sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT expires_at, accepted_at, role FROM invites WHERE token = ? AND team_id = ? FOR UPDATE`, request.Token, request.TeamID,
	).Scan(&expires, &accepted, &role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ErrNotFound
		}
		return err
	}
	if accepted.Valid {
		return model.ErrNotFound
	}
	expiresAt, err := ScanTime(expires)
	if err != nil {
		return fmt.Errorf("parse invite expiry: %w", err)
	}
	if !request.At.Before(expiresAt) {
		return model.ErrInviteExpired
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE invites SET accepted_at = ?, accepted_by = ? WHERE token = ? AND team_id = ? AND accepted_at IS NULL`,
		FormatTime(request.At), request.UserID, request.Token, request.TeamID)
	if err != nil {
		return fmt.Errorf("mark invite accepted: %w", err)
	}
	if count, err := res.RowsAffected(); err != nil {
		return err
	} else if count != 1 {
		return model.ErrNotFound
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		request.TeamID, request.UserID, role, FormatTime(request.At)); err != nil {
		return fmt.Errorf("add membership: %w", err)
	}
	return tx.Commit()
}

func scanTeamInvite(row interface{ Scan(...any) error }) (model.TeamInvite, error) {
	var invite model.TeamInvite
	var role, created, expires string
	var accepted sql.NullString
	var acceptedBy sql.NullInt64
	if err := row.Scan(&invite.Token, &invite.TeamID, &role, &invite.CreatedBy, &created, &expires, &accepted, &acceptedBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.TeamInvite{}, ErrNotFound
		}
		return model.TeamInvite{}, err
	}
	invite.Role = model.TeamRole(role)
	createdAt, err := ScanTime(created)
	if err != nil {
		return model.TeamInvite{}, fmt.Errorf("parse invitation creation time: %w", err)
	}
	invite.CreatedAt = createdAt
	invite.ExpiresAt, err = ScanTime(expires)
	if err != nil {
		return model.TeamInvite{}, fmt.Errorf("parse invitation expiry: %w", err)
	}
	if accepted.Valid {
		invite.AcceptedAt, err = ScanTime(accepted.String)
		if err != nil {
			return model.TeamInvite{}, fmt.Errorf("parse invitation acceptance time: %w", err)
		}
	}
	if acceptedBy.Valid {
		invite.AcceptedBy = acceptedBy.Int64
	}
	return invite, nil
}
