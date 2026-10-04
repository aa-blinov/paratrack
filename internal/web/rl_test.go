package web

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRateLimitKeyUsesFixedSizeDigest(t *testing.T) {
	input := "login|203.0.113.4|" + strings.Repeat("private-email-value", 100_000)
	got := hashRateLimitKey(input)
	if len(got) != 64 {
		t.Fatalf("hashed key length = %d, want 64", len(got))
	}
	if strings.Contains(got, "private-email-value") {
		t.Fatal("rate-limit digest retained its raw input")
	}
	if got != hashRateLimitKey(input) {
		t.Fatal("same rate-limit input produced different keys")
	}
}

func TestRateLimiterUnit(t *testing.T) {
	now := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	l := newRateLimiterWithClock(3, time.Minute, func() time.Time { return now })
	var ok []bool
	for i := 0; i < 5; i++ {
		ok = append(ok, l.allow("k"))
	}
	if ok[0] != true || ok[1] != true || ok[2] != true || ok[3] || ok[4] {
		t.Fatalf("allows = %v, want first three allowed and the rest denied", ok)
	}
	now = now.Add(time.Minute + time.Nanosecond)
	if !l.allow("k") {
		t.Fatal("request was still denied after the rate window expired")
	}
}

func TestRateLimiterBoundsDeniedAttemptHistory(t *testing.T) {
	const limit = 3
	now := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	l := newRateLimiterWithClock(limit, time.Minute, func() time.Time { return now })
	for i := 0; i < 10_000; i++ {
		l.allow("one-client")
	}
	if got, want := len(l.windows["one-client"]), limit+1; got != want {
		t.Fatalf("stored attempts = %d, want %d", got, want)
	}
}

func TestRateLimiterCollectsExpiredKeysDuringDeniedTraffic(t *testing.T) {
	const staleKeyCount = 10_000
	now := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	l := newRateLimiterWithClock(1, time.Minute, func() time.Time { return now })
	for i := 0; i < staleKeyCount; i++ {
		l.windows[fmt.Sprintf("stale-%d", i)] = []time.Time{now.Add(-2 * time.Minute)}
	}
	l.windows["hot"] = []time.Time{now}
	l.lastGC = now.Add(-2 * rateLimiterCleanupInterval)

	if l.allow("hot") {
		t.Fatal("request over the limit was allowed")
	}
	if _, exists := l.windows["stale-0"]; exists {
		t.Fatal("expired key was retained after a denied request")
	}
}

func TestRateLimiterBoundsDistinctKeysAndRecoversAfterCleanup(t *testing.T) {
	now := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	l := newRateLimiterWithClock(1, time.Minute, func() time.Time { return now })
	for i := 0; i < rateLimiterMaxKeys; i++ {
		l.windows[fmt.Sprintf("client-%d", i)] = []time.Time{now}
	}
	l.lastGC = now

	if l.allow("overflow-client") {
		t.Fatal("new key was allowed while the limiter table was full")
	}
	if _, exists := l.windows["overflow-client"]; exists {
		t.Fatal("overflow key was stored beyond the configured capacity")
	}
	if got := len(l.windows); got != rateLimiterMaxKeys {
		t.Fatalf("stored key count = %d, want %d", got, rateLimiterMaxKeys)
	}

	now = now.Add(time.Minute + time.Nanosecond)
	if !l.allow("overflow-client") {
		t.Fatal("new key remained blocked after expired keys were collected")
	}
	if got := len(l.windows); got != 1 {
		t.Fatalf("stored key count after cleanup = %d, want 1", got)
	}
}
