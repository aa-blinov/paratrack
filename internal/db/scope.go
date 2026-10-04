package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func actorOf(ctx context.Context) any {
	return requestctx.ActorValue(ctx)
}

// scopeSQL is the extra filter for a sessions alias ("s." or "").
func scopeSQL(ctx context.Context, col string, args []any) (string, []any) {
	if uid := requestctx.ScopedUserID(ctx); uid > 0 {
		return " AND " + col + " = ?", append(args, uid)
	}
	return "", args
}

func actorID(ctx context.Context) int64 {
	return requestctx.ActorID(ctx)
}

// lockSessionOwner serializes session-start/focus workflows for one actor.
// Legacy calls without an actor serialize at the team level when possible.
func lockSessionOwner(ctx context.Context, tx *Tx, teamID int64) error {
	query := ""
	var id int64
	if actor := actorID(ctx); actor > 0 {
		query = `SELECT id FROM users WHERE id = ? FOR UPDATE`
		if err := tx.QueryRowContext(ctx, query, actor).Scan(&id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		return nil
	}
	if teamID > 0 {
		query = `SELECT id FROM teams WHERE id = ? FOR UPDATE`
		if err := tx.QueryRowContext(ctx, query, teamID).Scan(&id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
	}
	return nil
}

// requireTeamMembership keeps a request authorized through its write
// transaction. Session mutations require a workspace and an authenticated
// member; unscoped legacy records are readable, but are never a write scope.
// It must follow lockSessionOwner so member removal and timer creation acquire
// user/membership locks in the same order.
func requireTeamMembership(ctx context.Context, tx *Tx, teamID int64) error {
	actor := actorID(ctx)
	if teamID <= 0 {
		return model.ErrForbidden
	}
	if actor <= 0 {
		return model.ErrForbidden
	}
	var memberID int64
	if err := tx.QueryRowContext(ctx,
		`SELECT user_id FROM memberships WHERE team_id = ? AND user_id = ? FOR SHARE`, teamID, actor).Scan(&memberID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ErrNotFound
		}
		return err
	}
	return nil
}
