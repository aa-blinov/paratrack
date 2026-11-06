package model

import "errors"

// ErrRequestInProgress means another attempt with the same Idempotency-Key still
// holds the key and has not finished, so the caller should retry later.
var ErrRequestInProgress = errors.New("request with this idempotency key is in progress")
