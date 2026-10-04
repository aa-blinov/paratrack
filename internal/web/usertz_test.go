package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequestClockUsesOneInjectedInstant(t *testing.T) {
	want := time.Date(2026, time.October, 2, 12, 30, 0, 0, time.UTC)
	calls := 0
	handler := withRequestClock(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first := userNow(r)
		second := userNow(r)
		if !first.Equal(want) || !second.Equal(want) {
			t.Errorf("request clock values = %s and %s, want %s", first, second, want)
		}
	}), func() time.Time {
		calls++
		return want
	})
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if calls != 1 {
		t.Fatalf("clock called %d times for one request, want 1", calls)
	}
}

func TestUserNowRequiresRequestClockMiddleware(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("userNow accepted a request without the clock middleware")
		}
	}()
	userNow(httptest.NewRequest(http.MethodGet, "/", nil))
}
