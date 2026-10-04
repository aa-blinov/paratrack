package db

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Secrets at rest (integration tokens, Stripe keys, webhook secrets) are
// sealed with AES-256-GCM under PARATRACK_SECRET_KEY. Stored form:
// "enc:v1:<base64(nonce|ciphertext)>". Keyless operation is allowed only
// where the process config explicitly permits it (development/test); plain
// legacy values remain readable and are sealed on startup once a key is set.
const sealedPrefix = "enc:v1:"

type secretCodec struct{ aead cipher.AEAD }

func newSecretCodec(key string) secretCodec {
	if key == "" {
		return secretCodec{}
	}
	sum := sha256.Sum256([]byte(key)) // any length passphrase → 32-byte key
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return secretCodec{}
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return secretCodec{}
	}
	return secretCodec{aead: gcm}
}

func (d *DB) sealSecret(plain string) (string, error) {
	gcm := d.secrets.aead
	if gcm == nil || plain == "" {
		return plain, nil
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate secret nonce: %w", err)
	}
	return sealedPrefix + base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil)), nil
}

var errSealedNoKey = errors.New("secret is encrypted but PARATRACK_SECRET_KEY is not set or wrong")

func (d *DB) openSecret(stored string) (string, error) {
	if !strings.HasPrefix(stored, sealedPrefix) {
		return stored, nil
	}
	gcm := d.secrets.aead
	if gcm == nil {
		return "", errSealedNoKey
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, sealedPrefix))
	if err != nil || len(raw) < gcm.NonceSize() {
		return "", errSealedNoKey
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", errSealedNoKey
	}
	return string(plain), nil
}

// mustOpen is openSecret for read paths: a value that can't be opened
// reads as empty (the integration then fails with a clear auth error)
// instead of leaking the sealed blob to a third-party API.
func (d *DB) mustOpen(stored string) string {
	s, err := d.openSecret(stored)
	if err != nil {
		d.logger.Printf("secrets: %v", err)
		return ""
	}
	return s
}

func (d *DB) sealExistingSecretsContext(ctx context.Context) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin secret migration: %w", err)
	}
	defer tx.Rollback()
	// Serialize key validation and plaintext sealing across process starts.
	// Without this lock, two replicas with different keys could both scan the
	// same plaintext and leave a mixture of ciphertexts encrypted by each key.
	if err := lockMigrations(ctx, tx); err != nil {
		return fmt.Errorf("lock secret migration: %w", err)
	}
	columns := []struct{ table, col string }{
		{"integrations", "secret"}, {"webhooks", "secret"},
		{"teams", "stripe_key"}, {"teams", "stripe_webhook_secret"},
	}
	sealPlaintext := d.secrets.aead != nil

	// Validate every existing ciphertext before changing any legacy plaintext.
	// Otherwise a wrong key discovered in a later table could leave earlier
	// tables newly encrypted with that wrong key. This pass retains no
	// plaintext values, so memory use does not grow with credential count.
	for _, c := range columns {
		rows, err := tx.QueryContext(ctx, `SELECT `+c.col+` FROM `+c.table+
			` WHERE `+c.col+` IS NOT NULL AND `+c.col+` <> ''`)
		if err != nil {
			return fmt.Errorf("read secrets from %s.%s: %w", c.table, c.col, err)
		}
		for rows.Next() {
			var value string
			if err := rows.Scan(&value); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan secret %s.%s: %w", c.table, c.col, err)
			}
			if strings.HasPrefix(value, sealedPrefix) {
				if _, err := d.openSecret(value); err != nil {
					_ = rows.Close()
					return fmt.Errorf("validate encrypted secret %s.%s: %w", c.table, c.col, err)
				}
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("iterate secrets from %s.%s: %w", c.table, c.col, err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close secrets from %s.%s: %w", c.table, c.col, err)
		}
	}
	// Invite tokens use a string primary key, so they do not participate in
	// the numeric-id plaintext sealing pass below. Validate their ciphertexts
	// here so a wrong key fails during startup instead of on the team page.
	rows, err := tx.QueryContext(ctx, `SELECT sealed_token FROM invites WHERE sealed_token <> ''`)
	if err != nil {
		return fmt.Errorf("read encrypted invite tokens: %w", err)
	}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan encrypted invite token: %w", err)
		}
		if _, err := d.openSecret(value); err != nil {
			_ = rows.Close()
			return fmt.Errorf("validate encrypted invite token: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate encrypted invite tokens: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close encrypted invite tokens: %w", err)
	}
	if !sealPlaintext {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit secret validation: %w", err)
		}
		return nil
	}

	sealedCounts := make(map[string]int)
	for _, c := range columns {
		var lastID int64
		for {
			rows, err := tx.QueryContext(ctx, `SELECT id, `+c.col+` FROM `+c.table+
				` WHERE id > ? AND `+c.col+` IS NOT NULL AND `+c.col+` <> '' AND `+c.col+` NOT LIKE ?`+
				` ORDER BY id LIMIT ?`, lastID, sealedPrefix+"%", migrationBatchSize)
			if err != nil {
				return fmt.Errorf("read plaintext secrets from %s.%s: %w", c.table, c.col, err)
			}
			type pendingSecret struct {
				id    int64
				value string
			}
			batch := make([]pendingSecret, 0, migrationBatchSize)
			for rows.Next() {
				var item pendingSecret
				if err := rows.Scan(&item.id, &item.value); err != nil {
					_ = rows.Close()
					return fmt.Errorf("scan plaintext secret %s.%s: %w", c.table, c.col, err)
				}
				batch = append(batch, item)
			}
			if err := rows.Err(); err != nil {
				_ = rows.Close()
				return fmt.Errorf("iterate plaintext secrets from %s.%s: %w", c.table, c.col, err)
			}
			if err := rows.Close(); err != nil {
				return fmt.Errorf("close plaintext secrets from %s.%s: %w", c.table, c.col, err)
			}
			if len(batch) == 0 {
				break
			}
			lastID = batch[len(batch)-1].id
			for _, item := range batch {
				sealed, err := d.sealSecret(item.value)
				if err != nil {
					return fmt.Errorf("seal secret %s.%s id %d: %w", c.table, c.col, item.id, err)
				}
				// Compare the scanned plaintext so concurrent credential changes
				// are never overwritten by a stale migration value.
				result, err := tx.ExecContext(ctx,
					`UPDATE `+c.table+` SET `+c.col+` = ? WHERE id = ? AND `+c.col+` = ?`, sealed, item.id, item.value)
				if err != nil {
					return fmt.Errorf("write sealed secret %s.%s id %d: %w", c.table, c.col, item.id, err)
				}
				updated, err := result.RowsAffected()
				if err != nil {
					return fmt.Errorf("count sealed secret updates for %s.%s id %d: %w", c.table, c.col, item.id, err)
				}
				sealedCounts[c.table+"."+c.col] += int(updated)
			}
		}
	}
	var sealedInviteCount int
	for {
		rows, err := tx.QueryContext(ctx, `SELECT token, sealed_token FROM invites WHERE sealed_token <> '' AND sealed_token NOT LIKE ? ORDER BY token LIMIT ?`, sealedPrefix+"%", migrationBatchSize)
		if err != nil {
			return fmt.Errorf("read plaintext invite tokens: %w", err)
		}
		type pendingInvite struct{ hash, token string }
		batch := make([]pendingInvite, 0, migrationBatchSize)
		for rows.Next() {
			var item pendingInvite
			if err := rows.Scan(&item.hash, &item.token); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan plaintext invite token: %w", err)
			}
			batch = append(batch, item)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("iterate plaintext invite tokens: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close plaintext invite tokens: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		for _, item := range batch {
			sealed, err := d.sealSecret(item.token)
			if err != nil {
				return fmt.Errorf("seal plaintext invite token: %w", err)
			}
			result, err := tx.ExecContext(ctx,
				`UPDATE invites SET sealed_token = ? WHERE token = ? AND sealed_token = ?`, sealed, item.hash, item.token)
			if err != nil {
				return fmt.Errorf("write sealed invite token: %w", err)
			}
			updated, err := result.RowsAffected()
			if err != nil {
				return fmt.Errorf("count sealed invite token updates: %w", err)
			}
			sealedInviteCount += int(updated)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit secret migration: %w", err)
	}
	for column, count := range sealedCounts {
		d.logger.Printf("secrets: sealed %d %s values", count, column)
	}
	if sealedInviteCount > 0 {
		d.logger.Printf("secrets: sealed %d invites.sealed_token values", sealedInviteCount)
	}
	return nil
}
