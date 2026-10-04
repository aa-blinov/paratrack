package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

var ErrAmbiguousProject = model.ErrAmbiguousProject

// FirstTeamID returns the lowest-ID workspace. It supports the CLI's
// single-user default while keeping SQL out of command adapters.
func (d *DB) FirstTeamID(ctx context.Context) (int64, error) {
	var id int64
	err := d.sql.QueryRowContext(ctx, `SELECT id FROM teams ORDER BY id LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("find first team: %w", err)
	}
	return id, nil
}

// ProjectIDBySlug resolves an unscoped CLI slug only when it identifies one
// project. Team-scoped slug collisions require the caller to use the numeric ID.
func (d *DB) ProjectIDBySlug(ctx context.Context, slug string) (int64, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT id FROM projects WHERE slug = ? ORDER BY id LIMIT 2`, slug)
	if err != nil {
		return 0, fmt.Errorf("find project by slug: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, fmt.Errorf("find project by slug: %w", err)
		}
		return 0, ErrNotFound
	}
	var id int64
	if err := rows.Scan(&id); err != nil {
		return 0, fmt.Errorf("read project id: %w", err)
	}
	if rows.Next() {
		return 0, ErrAmbiguousProject
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("find project by slug: %w", err)
	}
	return id, nil
}

// CreateOwnedTeam inserts a team and its owner membership atomically. A team
// must never become visible without the membership that grants its creator
// access to it.
func (d *DB) CreateOwnedTeam(ctx context.Context, request appmodel.TeamCreateRequest) (int64, error) {
	if request.OwnerID <= 0 || strings.TrimSpace(request.Name) == "" || strings.TrimSpace(request.Slug) == "" {
		return 0, model.ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin create team: %w", err)
	}
	defer tx.Rollback()

	now := FormatTime(d.currentTime().UTC())
	var teamID int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO teams (slug, name, owner_id, created_at) VALUES (?, ?, ?, ?) RETURNING id`,
		request.Slug, request.Name, request.OwnerID, now,
	).Scan(&teamID); err != nil {
		if isUniqueViolation(err) {
			return 0, model.ErrAlreadyExists
		}
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'owner', ?)`,
		teamID, request.OwnerID, now,
	); err != nil {
		return 0, fmt.Errorf("add owner membership: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit create team: %w", err)
	}
	return teamID, nil
}
