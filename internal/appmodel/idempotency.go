package appmodel

import "time"

// IdempotencyRequest names one replayed write: the user who sent it, the
// Idempotency-Key, the instant of the attempt and, when finishing, the status.
type IdempotencyRequest struct {
	UserID int64
	Key    string
	Status int
	Now    time.Time
}
