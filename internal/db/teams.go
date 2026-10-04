package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

func (d *DB) FindTeam(ctx context.Context, id int64) (model.Team, error) {
	return scanTeam(d.sql.QueryRowContext(ctx,
		`SELECT id, slug, name, owner_id, created_at FROM teams WHERE id = ?`, id))
}

func (d *DB) FindTeamBySlug(ctx context.Context, slug string) (model.Team, error) {
	return scanTeam(d.sql.QueryRowContext(ctx,
		`SELECT id, slug, name, owner_id, created_at FROM teams WHERE slug = ?`, slug))
}

func (d *DB) ListTeamsForUser(ctx context.Context, userID int64) ([]model.Team, error) {
	return listTeamsForUser(ctx, d.sql, userID)
}

type teamQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func listTeamsForUser(ctx context.Context, queryer teamQueryer, userID int64) ([]model.Team, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT t.id, t.slug, t.name, t.owner_id, t.created_at
		FROM teams t JOIN memberships m ON m.team_id = t.id
		WHERE m.user_id = ? ORDER BY (m.role = 'owner') DESC, m.joined_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var teams []model.Team
	for rows.Next() {
		team, err := scanTeam(rows)
		if err != nil {
			return nil, err
		}
		teams = append(teams, team)
	}
	return teams, rows.Err()
}

func (d *DB) ListMembershipsForUser(ctx context.Context, userID int64) ([]model.TeamMembership, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT t.id, t.slug, t.name, t.owner_id, t.created_at, m.role, m.joined_at
		FROM teams t JOIN memberships m ON m.team_id = t.id
		WHERE m.user_id = ? ORDER BY (m.role = 'owner') DESC, m.joined_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var memberships []model.TeamMembership
	for rows.Next() {
		var membership model.TeamMembership
		var createdAt, joinedAt string
		var role string
		if err := rows.Scan(&membership.Team.ID, &membership.Team.Slug, &membership.Team.Name,
			&membership.Team.OwnerID, &createdAt, &role, &joinedAt); err != nil {
			return nil, err
		}
		membership.Team.CreatedAt, err = ScanTime(createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse workspace creation time: %w", err)
		}
		membership.Role = model.TeamRole(role)
		membership.JoinedAt, err = ScanTime(joinedAt)
		if err != nil {
			return nil, fmt.Errorf("parse membership join time: %w", err)
		}
		memberships = append(memberships, membership)
	}
	return memberships, rows.Err()
}

func (d *DB) FindMembershipForUser(ctx context.Context, teamID, userID int64) (model.TeamMembership, bool, error) {
	var membership model.TeamMembership
	var createdAt, joinedAt, role string
	err := d.sql.QueryRowContext(ctx, `
		SELECT t.id, t.slug, t.name, t.owner_id, t.created_at, m.role, m.joined_at
		FROM teams t JOIN memberships m ON m.team_id = t.id
		WHERE m.team_id = ? AND m.user_id = ?`, teamID, userID).
		Scan(&membership.Team.ID, &membership.Team.Slug, &membership.Team.Name,
			&membership.Team.OwnerID, &createdAt, &role, &joinedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.TeamMembership{}, false, nil
	}
	if err != nil {
		return model.TeamMembership{}, false, err
	}
	membership.Team.CreatedAt, err = ScanTime(createdAt)
	if err != nil {
		return model.TeamMembership{}, false, fmt.Errorf("parse workspace creation time: %w", err)
	}
	membership.Role = model.TeamRole(role)
	membership.JoinedAt, err = ScanTime(joinedAt)
	if err != nil {
		return model.TeamMembership{}, false, fmt.Errorf("parse membership join time: %w", err)
	}
	return membership, true, nil
}

func (d *DB) RenameTeam(ctx context.Context, request appmodel.TeamRenameRequest) error {
	return d.execManagerTeamUpdate(ctx, request.TeamID, request.CallerID, `UPDATE teams SET name = ? WHERE id = ?`, request.Name, request.TeamID)
}

func (d *DB) CountTeamMembers(ctx context.Context, teamID int64) (int, error) {
	var count int
	err := d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM memberships WHERE team_id = ?`, teamID).Scan(&count)
	return count, err
}

// DeleteTeamAndListRemaining atomically deletes a workspace and loads the
// caller's new workspace selection before committing the deletion.
func (d *DB) DeleteTeamAndListRemaining(ctx context.Context, request appmodel.WorkspaceDeleteRequest) ([]model.Team, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := deleteTeamTx(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return nil, err
	}
	remaining, err := listTeamsForUser(ctx, tx, request.CallerID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return remaining, nil
}

func deleteTeamTx(ctx context.Context, tx *Tx, teamID, callerID int64) error {
	var ownerID int64
	if err := tx.QueryRowContext(ctx, `SELECT owner_id FROM teams WHERE id = ? FOR UPDATE`, teamID).Scan(&ownerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ErrNotFound
		}
		return err
	}
	if ownerID != callerID {
		return model.ErrForbidden
	}
	var members int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memberships WHERE team_id = ?`, teamID).Scan(&members); err != nil {
		return err
	}
	if members > 1 {
		return model.ErrTeamHasMembers
	}
	if err := execRequireRows(ctx, tx, `DELETE FROM teams WHERE id = ? AND owner_id = ?`, teamID, callerID); err != nil {
		return err
	}
	return nil
}

func (d *DB) TeamMemberRole(ctx context.Context, teamID, userID int64) (model.TeamRole, bool, error) {
	var role string
	err := d.sql.QueryRowContext(ctx,
		`SELECT role FROM memberships WHERE team_id = ? AND user_id = ?`, teamID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return model.TeamRole(role), true, nil
}

func (d *DB) ListTeamMembers(ctx context.Context, teamID int64) ([]model.TeamMember, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT u.id, u.email, u.name, m.role, m.joined_at
		FROM memberships m JOIN users u ON u.id = m.user_id
		WHERE m.team_id = ? ORDER BY (m.role = 'owner') DESC, m.joined_at`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []model.TeamMember
	for rows.Next() {
		var member model.TeamMember
		var role, joined string
		if err := rows.Scan(&member.UserID, &member.Email, &member.Name, &role, &joined); err != nil {
			return nil, err
		}
		member.Role = model.TeamRole(role)
		member.JoinedAt, err = ScanTime(joined)
		if err != nil {
			return nil, fmt.Errorf("parse membership join time: %w", err)
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func scanTeam(row interface{ Scan(...any) error }) (model.Team, error) {
	var team model.Team
	var created string
	if err := row.Scan(&team.ID, &team.Slug, &team.Name, &team.OwnerID, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Team{}, ErrNotFound
		}
		return model.Team{}, err
	}
	createdAt, err := ScanTime(created)
	if err != nil {
		return model.Team{}, fmt.Errorf("parse workspace creation time: %w", err)
	}
	team.CreatedAt = createdAt
	return team, nil
}
