package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/aa-blinov/paratrack/internal/model"
)

// lockTeamManager serializes a privileged write with workspace ownership,
// role changes and member removal. Call it before locking feature rows.
func lockTeamManager(ctx context.Context, tx *Tx, teamID, callerID int64) (int64, model.TeamRole, error) {
	ownerID, role, err := lockTeamRole(ctx, tx, teamID, callerID)
	if err != nil {
		return 0, "", err
	}
	if !role.CanManage() || (callerID == ownerID && role != model.TeamRoleOwner) {
		return 0, "", model.ErrForbidden
	}
	return ownerID, role, nil
}

// lockTeamRole serializes an actor-scoped write with workspace membership and
// role changes, returning the current role while both rows remain locked.
func lockTeamRole(ctx context.Context, tx *Tx, teamID, callerID int64) (int64, model.TeamRole, error) {
	if teamID <= 0 || callerID <= 0 {
		return 0, "", model.ErrForbidden
	}
	var ownerID int64
	if err := tx.QueryRowContext(ctx,
		`SELECT owner_id FROM teams WHERE id = ? FOR UPDATE`, teamID).Scan(&ownerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", model.ErrNotFound
		}
		return 0, "", err
	}
	var role string
	if err := tx.QueryRowContext(ctx,
		`SELECT role FROM memberships WHERE team_id = ? AND user_id = ? FOR UPDATE`, teamID, callerID).Scan(&role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", model.ErrForbidden
		}
		return 0, "", err
	}
	return ownerID, model.TeamRole(role), nil
}

// lockCurrentTeamMember serializes member-scoped writes with member removal
// and rechecks the actor while the workspace row is locked.
func lockCurrentTeamMember(ctx context.Context, tx *Tx, teamID, callerID int64) error {
	if teamID <= 0 || callerID <= 0 {
		return model.ErrForbidden
	}
	var lockedTeam int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM teams WHERE id = ? FOR UPDATE`, teamID).Scan(&lockedTeam); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ErrNotFound
		}
		return err
	}
	var memberID int64
	if err := tx.QueryRowContext(ctx,
		`SELECT user_id FROM memberships WHERE team_id = ? AND user_id = ? FOR UPDATE`, teamID, callerID).Scan(&memberID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ErrForbidden
		}
		return err
	}
	return nil
}
