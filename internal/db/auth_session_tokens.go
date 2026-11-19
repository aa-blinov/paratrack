package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// authSessionTokenHash is how a browser session is stored: the digest of the
// cookie token, never the token itself, so a leaked dump cannot be replayed
// as logins. Raw tokens are 43 characters of base64, digests are 64 hex, so
// the two forms cannot collide.
func authSessionTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// migrateAuthSessionTokensContext hashes every session token still stored raw.
// The runtime lookup accepts both forms, so nothing breaks mid-deploy; a row
// nobody touches again would stay plaintext forever, though, and this pass
// makes the whole table clean at once, so a backup taken after it holds no
// usable credentials. Already-hashed rows are 64 hex characters and drop out
// of the batch filter.
func (d *DB) migrateAuthSessionTokensContext(ctx context.Context) error {
	_, err := d.runDataMigration(ctx, "20261119_hash_auth_session_tokens", func(tx *Tx) error {
		for {
			rows, err := tx.QueryContext(ctx, `SELECT token FROM auth_sessions WHERE length(token) <> 64 ORDER BY token LIMIT ?`, migrationBatchSize)
			if err != nil {
				return fmt.Errorf("read raw session tokens: %w", err)
			}
			var tokens []string
			for rows.Next() {
				var token string
				if err := rows.Scan(&token); err != nil {
					_ = rows.Close()
					return fmt.Errorf("scan raw session token: %w", err)
				}
				tokens = append(tokens, token)
			}
			if err := rows.Err(); err != nil {
				_ = rows.Close()
				return fmt.Errorf("iterate raw session tokens: %w", err)
			}
			if err := rows.Close(); err != nil {
				return fmt.Errorf("close raw session tokens: %w", err)
			}
			if len(tokens) == 0 {
				return nil
			}
			for _, token := range tokens {
				if _, err := tx.ExecContext(ctx, `UPDATE auth_sessions SET token = ? WHERE token = ?`, authSessionTokenHash(token), token); err != nil {
					return fmt.Errorf("hash raw session token: %w", err)
				}
			}
		}
	})
	return err
}
