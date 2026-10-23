package db

import (
	"context"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// RotateWebhookSecret stores a webhook's new signing secret, sealed like the first one, for a
// manager of the workspace (ADM-13).
func (d *DB) RotateWebhookSecret(ctx context.Context, request appmodel.WebhookRotateRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.WebhookID <= 0 || request.Secret == "" {
		return ErrNotFound
	}
	sealed, err := d.sealSecret(request.Secret)
	if err != nil {
		return err
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockTeamOwner(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE webhooks SET secret = ? WHERE id = ? AND team_id = ?`, sealed, request.WebhookID, request.TeamID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}
