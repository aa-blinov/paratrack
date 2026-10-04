package web

import "net/http"

// cacheStatic sets a modest immutable-ish policy for vendored assets.
// Filenames do not change when content does (single css / js names), so
// we use a one-day TTL rather than `immutable` — a deploy picks up new
// bytes within a day, and repeat visits skip the network.
func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Versioned URLs (?v=assetVersion) never change content: cache for good.
		if r.URL.Query().Get("v") != "" {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}
		w.Header().Set("Vary", "Accept-Encoding")
		next.ServeHTTP(w, r)
	})
}
