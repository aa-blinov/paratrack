package auth

import (
	"testing"
	"time"
)

func TestExpiredAt(t *testing.T) {
	expiresAt := time.Date(2026, time.October, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		now  time.Time
		want bool
	}{
		{name: "before expiry", now: expiresAt.Add(-time.Nanosecond), want: false},
		{name: "at expiry", now: expiresAt, want: true},
		{name: "after expiry", now: expiresAt.Add(time.Nanosecond), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := expiredAt(expiresAt, tt.now); got != tt.want {
				t.Fatalf("expiredAt() = %t, want %t", got, tt.want)
			}
		})
	}
}
