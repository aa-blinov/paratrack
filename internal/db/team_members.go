package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

func (d *DB) SetTeamMemberRole(ctx context.Context, request appmodel.TeamMemberRoleRequest) (bool, error) {
	if request.TeamID <= 0 || request.TargetUserID <= 0 || request.CallerID <= 0 {
		return false, ErrNotFound
	}
	teamID, userID, callerID, role := request.TeamID, request.TargetUserID, request.CallerID, request.Role
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	ownerID, _, err := lockTeamManager(ctx, tx, teamID, callerID)
	if err != nil {
		return false, err
	}
	if ownerID != callerID {
		return false, model.ErrForbidden
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE memberships SET role = ? WHERE team_id = ? AND user_id = ? AND role <> 'owner'`, role, teamID, userID)
	if err != nil {
		return false, err
	}
	updated, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if updated == 0 {
		return false, nil
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (d *DB) TransferTeamOwnership(ctx context.Context, request appmodel.TeamOwnershipTransferRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.NewOwnerID <= 0 {
		return ErrNotFound
	}
	teamID, oldOwnerID, newOwnerID := request.TeamID, request.CallerID, request.NewOwnerID
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var currentOwnerID int64
	if err := tx.QueryRowContext(ctx, `SELECT owner_id FROM teams WHERE id = ? FOR UPDATE`, teamID).Scan(&currentOwnerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ErrNotFound
		}
		return err
	}
	if currentOwnerID != oldOwnerID {
		return model.ErrNotFound
	}
	var memberID int64
	if err := tx.QueryRowContext(ctx,
		`SELECT user_id FROM memberships WHERE team_id = ? AND user_id = ?`, teamID, newOwnerID).Scan(&memberID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ErrNotFound
		}
		return err
	}
	for _, operation := range []struct {
		query string
		args  []any
	}{
		{`UPDATE memberships SET role = 'admin' WHERE team_id = ? AND user_id = ?`, []any{teamID, oldOwnerID}},
		{`UPDATE memberships SET role = 'owner' WHERE team_id = ? AND user_id = ?`, []any{teamID, newOwnerID}},
		{`UPDATE teams SET owner_id = ? WHERE id = ? AND owner_id = ?`, []any{newOwnerID, teamID, oldOwnerID}},
	} {
		res, err := tx.ExecContext(ctx, operation.query, operation.args...)
		if err != nil {
			return err
		}
		if affected, err := res.RowsAffected(); err != nil {
			return err
		} else if affected != 1 {
			return model.ErrNotFound
		}
	}
	return tx.Commit()
}

func (d *DB) CountTeamOwners(ctx context.Context, teamID int64) (int, error) {
	var count int
	err := d.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memberships WHERE team_id = ? AND role = 'owner'`, teamID).Scan(&count)
	return count, err
}

func (d *DB) RemoveTeamMember(ctx context.Context, request appmodel.TeamMemberRemovalRequest) error {
	if request.TeamID <= 0 || request.TargetUserID <= 0 || request.CallerID <= 0 || request.LeftAt.IsZero() {
		return ErrNotFound
	}
	teamID, userID, callerID := request.TeamID, request.TargetUserID, request.CallerID
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var teamOwnerID int64
	if err := tx.QueryRowContext(ctx, `SELECT owner_id FROM teams WHERE id = ? FOR UPDATE`, teamID).Scan(&teamOwnerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ErrNotFound
		}
		return err
	}
	var lockedUser int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id = ? FOR UPDATE`, userID).Scan(&lockedUser); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ErrNotFound
		}
		return fmt.Errorf("lock removed member: %w", err)
	}
	var callerRole, targetRole string
	if err := tx.QueryRowContext(ctx,
		`SELECT role FROM memberships WHERE team_id = ? AND user_id = ? FOR UPDATE`, teamID, callerID).Scan(&callerRole); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ErrForbidden
		}
		return err
	}
	if callerID != userID && callerRole != string(model.TeamRoleOwner) && callerRole != string(model.TeamRoleAdmin) {
		return model.ErrForbidden
	}
	if err := tx.QueryRowContext(ctx,
		`SELECT role FROM memberships WHERE team_id = ? AND user_id = ? FOR UPDATE`, teamID, userID).Scan(&targetRole); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ErrNotFound
		}
		return err
	}
	if callerID != userID && targetRole == string(model.TeamRoleOwner) && callerRole != string(model.TeamRoleOwner) {
		return model.ErrForbidden
	}
	if teamOwnerID == userID {
		return model.ErrOwnerMustTransfer
	}
	if err := d.stopMemberSessionsTx(ctx, tx, teamID, userID, request.LeftAt); err != nil {
		return fmt.Errorf("stop removed member sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM former_members WHERE team_id = ? AND user_id = ?`, teamID, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO former_members (team_id, user_id, hourly_pay_cents, left_at)
		SELECT team_id, user_id, hourly_pay_cents, ? FROM memberships WHERE team_id = ? AND user_id = ?`,
		FormatTime(request.LeftAt), teamID, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE team_id = ? AND user_id = ?`, teamID, userID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM memberships WHERE team_id = ? AND user_id = ?`, teamID, userID)
	if err != nil {
		return err
	}
	if affected, err := res.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return model.ErrNotFound
	}
	return tx.Commit()
}
