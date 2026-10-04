package auth

import "time"

// expiredAt treats a credential as invalid at its expiry instant.
func expiredAt(expiresAt, now time.Time) bool {
	return !now.Before(expiresAt)
}
