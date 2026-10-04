// Package httpjson decodes bounded JSON payloads received from HTTP peers.
package httpjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
)

const (
	MaxRequestBytes  int64 = 1 << 20
	MaxResponseBytes int64 = 8 << 20
)

var (
	ErrInvalidLimit    = errors.New("JSON payload size limit must be positive")
	ErrPayloadTooLarge = errors.New("JSON payload exceeds size limit")
)

// Decode reads at most maxBytes+1 bytes before decoding. The extra byte makes
// oversized bodies distinguishable from valid JSON truncated at the limit.
func Decode(reader io.Reader, maxBytes int64, target any) error {
	if maxBytes <= 0 || maxBytes == math.MaxInt64 {
		return ErrInvalidLimit
	}
	payload, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return fmt.Errorf("read JSON payload: %w", err)
	}
	if int64(len(payload)) > maxBytes {
		return fmt.Errorf("%w: limit is %d bytes", ErrPayloadTooLarge, maxBytes)
	}
	if err := json.Unmarshal(bytes.TrimSpace(payload), target); err != nil {
		return fmt.Errorf("decode JSON payload: %w", err)
	}
	return nil
}
