package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// ListFormerMembers returns the people who left the workspace and are not back in
// it, newest departure first (ADM-04).
func (d *DB) ListFormerMembers(ctx context.Context, teamID int64) ([]model.FormerMember, error) {
	if teamID <= 0 {
		return nil, ErrNotFound
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT f.user_id, u.email, u.name, f.left_at
		   FROM former_members f
		   JOIN users u ON u.id = f.user_id
		  WHERE f.team_id = ?
		    AND NOT EXISTS (SELECT 1 FROM memberships m WHERE m.team_id = f.team_id AND m.user_id = f.user_id)
		  ORDER BY f.left_at DESC`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.FormerMember
	for rows.Next() {
		var member model.FormerMember
		var leftAt any
		if err := rows.Scan(&member.UserID, &member.Email, &member.Name, &leftAt); err != nil {
			return nil, err
		}
		if member.LeftAt, err = ScanTime(leftAt); err != nil {
			return nil, err
		}
		out = append(out, member)
	}
	return out, rows.Err()
}

// ReturnTeamMember brings a former member back as a plain member, with the pay they
// left with. Only someone who manages the workspace can do it, and the check runs in
// the transaction. A person who is still in the workspace cannot be returned.
func (d *DB) ReturnTeamMember(ctx context.Context, request appmodel.TeamMemberReturnRequest) error {
	if request.TeamID <= 0 || request.TargetUserID <= 0 || request.CallerID <= 0 {
		return ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return err
	}
	var pay sql.NullInt64
	err = tx.QueryRowContext(ctx,
		`SELECT hourly_pay_cents FROM former_members WHERE team_id = ? AND user_id = ? FOR UPDATE`,
		request.TeamID, request.TargetUserID).Scan(&pay)
	if errors.Is(err, sql.ErrNoRows) {
		return model.ErrNotFound
	}
	if err != nil {
		return err
	}
	var stillMember int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memberships WHERE team_id = ? AND user_id = ?`,
		request.TeamID, request.TargetUserID).Scan(&stillMember); err != nil {
		return err
	}
	if stillMember > 0 {
		return model.ErrAlreadyExists
	}
	var name string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM users WHERE id = ?`, request.TargetUserID).Scan(&name); err != nil {
		return err
	}
	now := FormatTime(d.currentTime().UTC())
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at, hourly_pay_cents) VALUES (?, ?, 'member', ?, ?)`,
		request.TeamID, request.TargetUserID, now, pay); err != nil {
		return fmt.Errorf("add returned member: %w", err)
	}
	// The membership holds the pay from now on; the departure row would count it twice.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM former_members WHERE team_id = ? AND user_id = ?`, request.TeamID, request.TargetUserID); err != nil {
		return err
	}
	if err := auditInTx(ctx, tx, request.TeamID, request.CallerID, "member.return", name,
		fmt.Sprintf("user:%d", request.TargetUserID), now); err != nil {
		return err
	}
	return tx.Commit()
}
