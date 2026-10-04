package web

import (
	"context"
	"net/http"
	"sync"
	"time"
	_ "time/tzdata" // zones load in a slim container
)

// The server runs in UTC; people don't. Every "now", "today" and printed
// time uses the user's zone: the browser reports it in the paratrack_tz
// cookie (base.html), then the process-configured fallback before that first
// report, else the server's own zone.
var tzCache sync.Map // name → *time.Location

type defaultTimezoneKey struct{}
type requestClockKey struct{}

func withRequestClock(next http.Handler, now func() time.Time) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), requestClockKey{}, now())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func withDefaultTimezone(next http.Handler, name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), defaultTimezoneKey{}, name)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func loadZone(name string) *time.Location {
	if name == "" {
		return nil
	}
	if l, ok := tzCache.Load(name); ok {
		return l.(*time.Location)
	}
	l, err := time.LoadLocation(name)
	if err != nil {
		return nil
	}
	tzCache.Store(name, l)
	return l
}

func userLoc(r *http.Request) *time.Location {
	if l := loadZone(prefsOf(r).TZ); l != nil {
		return l // set by hand in settings: wins over the browser
	}
	if r != nil {
		if c, err := r.Cookie("paratrack_tz"); err == nil {
			if l := loadZone(c.Value); l != nil {
				return l
			}
		}
	}
	if r != nil {
		if name, _ := r.Context().Value(defaultTimezoneKey{}).(string); name != "" {
			if l := loadZone(name); l != nil {
				return l
			}
		}
	}
	return time.Local
}

// userNow returns the request's single clock instant in the user's zone.
// Routes must be wrapped with withRequestClock so handlers never select a
// process clock independently.
func userNow(r *http.Request) time.Time {
	if r == nil {
		panic("web: userNow called without an HTTP request")
	}
	now, ok := r.Context().Value(requestClockKey{}).(time.Time)
	if !ok {
		panic("web: request clock middleware is missing")
	}
	return now.In(userLoc(r))
}
