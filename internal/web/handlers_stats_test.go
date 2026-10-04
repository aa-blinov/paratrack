package web

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestParsePeriodAtUsesSuppliedRequestTime(t *testing.T) {
	location := time.FixedZone("UTC+3", 3*60*60)
	now := time.Date(2026, 10, 1, 0, 0, 1, 0, location)
	request := httptest.NewRequest("GET", "/stats?period=today", nil)
	period := (&Server{}).parsePeriodAt(request, now)
	wantStart := time.Date(2026, 10, 1, 0, 0, 0, 0, location)
	if !period.Start.Equal(wantStart) || !period.End.Equal(now) {
		t.Fatalf("period = %s..%s, want %s..%s", period.Start, period.End, wantStart, now)
	}
}
