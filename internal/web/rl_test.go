package web

import (
	"testing"
	"time"
)

func TestRateLimiterUnit(t *testing.T) {
	l := newRateLimiter(3, time.Minute)
	ok := []bool{}
	for i := 0; i < 5; i++ {
		ok = append(ok, l.allow("k"))
	}
	t.Logf("allows=%v", ok)
	if ok[3] || ok[4] {
		t.Fatalf("should deny 4th and 5th, got %v", ok)
	}
}
