package web

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	rateLimiterCleanupInterval = time.Minute
	rateLimiterMaxKeys         = 10_000
)

// ---------------------------------------------------------------------------
// Rate limiting — sliding window per key, in-memory.
//
// Good enough for a single-node deploy. Behind multiple replicas put a
// shared limiter (Redis) in front; this protects the common
// single-binary self-host and the auth endpoints from casual abuse.
// ---------------------------------------------------------------------------

type rateLimiter struct {
	mu      sync.Mutex
	windows map[string][]time.Time
	limit   int
	window  time.Duration
	lastGC  time.Time
	now     func() time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return newRateLimiterWithClock(limit, window, time.Now)
}

func newRateLimiterWithClock(limit int, window time.Duration, now func() time.Time) *rateLimiter {
	if limit < 0 {
		limit = 0
	}
	if now == nil {
		now = time.Now
	}
	return &rateLimiter{windows: map[string][]time.Time{}, limit: limit, window: window, now: now}
}

// allow records a hit for key and reports whether it fits the budget.
func (l *rateLimiter) allow(key string) bool {
	now := l.now()
	cut := now.Add(-l.window)
	l.mu.Lock()
	defer l.mu.Unlock()
	hits, knownKey := l.windows[key]
	if !knownKey && len(l.windows) >= rateLimiterMaxKeys {
		l.collectExpired(now, cut)
		if len(l.windows) >= rateLimiterMaxKeys {
			return false
		}
	}
	// Drop expired from the front; keep the rest and record this hit.
	i := 0
	for ; i < len(hits) && hits[i].Before(cut); i++ {
	}
	hits = append(hits[i:], now)
	// Keep only the latest limit+1 attempts. Extra denied requests are still
	// represented by a full window, but cannot grow one key's history forever.
	if maxHits := l.limit + 1; len(hits) > maxHits {
		hits = hits[len(hits)-maxHits:]
	}
	l.windows[key] = hits
	allowed := len(hits) <= l.limit
	l.collectExpired(now, cut)
	return allowed
}

func (l *rateLimiter) collectExpired(now, cut time.Time) {
	if len(l.windows) >= rateLimiterMaxKeys &&
		(l.lastGC.IsZero() || now.Sub(l.lastGC) >= rateLimiterCleanupInterval) {
		for k, v := range l.windows {
			if len(v) == 0 || v[len(v)-1].Before(cut) {
				delete(l.windows, k)
			}
		}
		l.lastGC = now
	}
}

// rateLimit returns middleware that allows `limit` requests per `window`
// per (ip + bucket). bucket lets login/register share a table without
// colliding. With a field ("email"), the key is ip + that form value, so
// a whole office behind one address can still sign in at nine while
// guessing one account's password stays slow.
func (s *Server) rateLimit(bucket string, limit int, window time.Duration, field ...string) func(http.Handler) http.Handler {
	lim := newRateLimiter(limit, window)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := bucket + "|" + clientIP(r)
			for _, f := range field {
				key += "|" + strings.ToLower(strings.TrimSpace(r.FormValue(f)))
			}
			if !lim.allow(hashRateLimitKey(key)) {
				w.Header().Set("Retry-After", "60")
				if strings.HasPrefix(r.URL.Path, "/api/") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = w.Write([]byte(`{"error":"too many requests"}`))
					return
				}
				http.Error(w, "Too many attempts. Try again in a minute.", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func hashRateLimitKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
