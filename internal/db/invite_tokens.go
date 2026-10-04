package db

import (
	"context"
	"fmt"
)

func (d *DB) migrateInviteTokensContext(ctx context.Context) error {
	_, err := d.runDataMigration(ctx, "20261004_hash_invite_tokens", func(tx *Tx) error {
		for {
			rows, err := tx.QueryContext(ctx, `SELECT token FROM invites WHERE sealed_token = '' ORDER BY token LIMIT ?`, migrationBatchSize)
			if err != nil {
				return fmt.Errorf("read legacy invite tokens: %w", err)
			}
			var tokens []string
			for rows.Next() {
				var token string
				if err := rows.Scan(&token); err != nil {
					_ = rows.Close()
					return fmt.Errorf("scan legacy invite token: %w", err)
				}
				tokens = append(tokens, token)
			}
			if err := rows.Err(); err != nil {
				_ = rows.Close()
				return fmt.Errorf("iterate legacy invite tokens: %w", err)
			}
			if err := rows.Close(); err != nil {
				return fmt.Errorf("close legacy invite tokens: %w", err)
			}
			if len(tokens) == 0 {
				return nil
			}
			for _, token := range tokens {
				sealed, err := d.sealSecret(token)
				if err != nil {
					return fmt.Errorf("seal legacy invite token: %w", err)
				}
				if _, err := tx.ExecContext(ctx, `UPDATE invites SET token = ?, sealed_token = ? WHERE token = ?`, teamInviteTokenHash(token), sealed, token); err != nil {
					return fmt.Errorf("hash legacy invite token: %w", err)
				}
			}
		}
	})
	return err
}
