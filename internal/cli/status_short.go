package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// statusShortTimeout bounds `status --short`. It runs inside a shell prompt
// or a tmux status line, where the shared 15-second client timeout froze the
// terminal whenever the network was slow.
const statusShortTimeout = 400 * time.Millisecond

// statusCache is the last answer `status --short` got from the server. When
// the server cannot be reached in time, the line is drawn from it, with the
// running timers moved forward by the time since, and marked as not fresh.
type statusCache struct {
	FetchedAt int64             `json:"fetched_at"`
	Items     []statusCacheItem `json:"items"`
}

type statusCacheItem struct {
	Activity string `json:"activity"`
	Seconds  int    `json:"seconds"`
	Paused   bool   `json:"paused"`
}

// remoteStatusShort prints one line for a prompt and always exits zero: a
// prompt that turns red or prints an error on every keystroke while offline
// is worse than one that says it is offline.
func (rt *Runtime) remoteStatusShort() error {
	ctx, cancel := context.WithTimeout(rt.Context, statusShortTimeout)
	defer cancel()
	now := rt.now()
	if _, body, err := rt.remoteDoContext(ctx, http.MethodGet, "/api/active", nil); err == nil {
		var res remoteActiveResponse
		if err := json.Unmarshal(body, &res); err == nil {
			cache := statusCache{FetchedAt: now.Unix()}
			for _, item := range res.Items {
				cache.Items = append(cache.Items, statusCacheItem{Activity: item.ActivityName, Seconds: item.DurationSecs, Paused: item.Paused})
			}
			rt.writeStatusCache(cache)
			fmt.Fprintln(rt.Out, formatStatusShort(cache.Items, 0, false))
			return nil
		}
	}
	cache, ok := rt.readStatusCache()
	if !ok {
		fmt.Fprintln(rt.Out, "offline")
		return nil
	}
	elapsed := int(now.Unix() - cache.FetchedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	fmt.Fprintln(rt.Out, formatStatusShort(cache.Items, elapsed, true))
	return nil
}

// formatStatusShort draws the line: "API work 1h05m", "idle", and a trailing
// "?" when the numbers come from the cache rather than from the server.
func formatStatusShort(items []statusCacheItem, elapsed int, stale bool) string {
	line := "idle"
	if len(items) > 0 {
		parts := make([]string, 0, len(items))
		for _, item := range items {
			secs := item.Seconds
			if !item.Paused {
				secs += elapsed
			}
			label := item.Activity + " " + formatShortDuration(secs)
			if item.Paused {
				label = item.Activity + " (paused) " + formatShortDuration(secs)
			}
			parts = append(parts, label)
		}
		line = strings.Join(parts, " | ")
	}
	if stale {
		line += " ?"
	}
	return line
}

func formatShortDuration(secs int) string {
	if secs < 0 {
		secs = 0
	}
	h, m := secs/3600, secs%3600/60
	if h == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dh%02dm", h, m)
}

func (rt *Runtime) readStatusCache() (statusCache, bool) {
	var cache statusCache
	if strings.TrimSpace(rt.StatusCachePath) == "" {
		return cache, false
	}
	data, err := os.ReadFile(rt.StatusCachePath)
	if err != nil || json.Unmarshal(data, &cache) != nil || cache.FetchedAt == 0 {
		return statusCache{}, false
	}
	return cache, true
}

// writeStatusCache is best effort: a prompt line never fails because the
// cache directory is read-only.
func (rt *Runtime) writeStatusCache(cache statusCache) {
	if strings.TrimSpace(rt.StatusCachePath) == "" {
		return
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(rt.StatusCachePath), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(rt.StatusCachePath, data, 0o600)
}

// dropStatusCache forgets the cached line after a command that changed the
// timers, so an offline prompt never shows a timer that was just stopped.
func (rt *Runtime) dropStatusCache() {
	if strings.TrimSpace(rt.StatusCachePath) != "" {
		_ = os.Remove(rt.StatusCachePath)
	}
}
