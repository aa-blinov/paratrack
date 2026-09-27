package db

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log"
	"os"
	"strings"
)

// Secrets at rest (integration tokens, Stripe keys, webhook secrets) are
// sealed with AES-256-GCM under PARATRACK_SECRET_KEY. Stored form:
// "enc:v1:<base64(nonce|ciphertext)>". Without a key values stay plain,
// as before; plain legacy values are always readable, and are sealed on
// startup once a key is set.
const sealedPrefix = "enc:v1:"

func secretAEAD() cipher.AEAD {
	k := os.Getenv("PARATRACK_SECRET_KEY")
	if k == "" {
		return nil
	}
	sum := sha256.Sum256([]byte(k)) // any length passphrase → 32-byte key
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil
	}
	return gcm
}

func sealSecret(plain string) string {
	gcm := secretAEAD()
	if gcm == nil || plain == "" || strings.HasPrefix(plain, sealedPrefix) {
		return plain
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return plain
	}
	return sealedPrefix + base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil))
}

var errSealedNoKey = errors.New("secret is encrypted but PARATRACK_SECRET_KEY is not set or wrong")

func openSecret(stored string) (string, error) {
	if !strings.HasPrefix(stored, sealedPrefix) {
		return stored, nil
	}
	gcm := secretAEAD()
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
func mustOpen(stored string) string {
	s, err := openSecret(stored)
	if err != nil {
		log.Printf("secrets: %v", err)
		return ""
	}
	return s
}

// sealExistingSecrets encrypts plain values left from before the key was
// set. Idempotent: sealed values are skipped.
func (d *DB) sealExistingSecrets() error {
	if secretAEAD() == nil {
		return nil
	}
	for _, c := range []struct{ table, col string }{
		{"integrations", "secret"}, {"webhooks", "secret"},
		{"teams", "stripe_key"}, {"teams", "stripe_webhook_secret"},
	} {
		rows, err := d.sql.QueryContext(context.Background(), `SELECT id, ` + c.col + ` FROM ` + c.table +
			` WHERE ` + c.col + ` IS NOT NULL AND ` + c.col + ` <> '' AND ` + c.col + ` NOT LIKE 'enc:v1:%'`)
		if err != nil {
			return err
		}
		type row struct {
			id int64
			v  string
		}
		var todo []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.id, &r.v); err != nil {
				rows.Close()
				return err
			}
			todo = append(todo, r)
		}
		rows.Close()
		for _, r := range todo {
			if _, err := d.sql.ExecContext(context.Background(),
				`UPDATE `+c.table+` SET `+c.col+` = ? WHERE id = ?`, sealSecret(r.v), r.id); err != nil {
				return err
			}
		}
		if len(todo) > 0 {
			log.Printf("secrets: sealed %d %s.%s values", len(todo), c.table, c.col)
		}
	}
	return nil
}
