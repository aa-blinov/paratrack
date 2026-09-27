package web

import (
	"net/http"
	"os"
	"sync"
	"time"
	_ "time/tzdata" // zones load in a slim container
)

// The server runs in UTC; people don't. Every "now", "today" and printed
// time uses the user's zone: the browser reports it in the paratrack_tz
// cookie (base.html), PARATRACK_TZ is the fallback before that first
// report (Europe/Moscow on the RU-first public instance), else the
// server's own zone.
var tzCache sync.Map // name → *time.Location

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
	if r != nil {
		if c, err := r.Cookie("paratrack_tz"); err == nil {
			if l := loadZone(c.Value); l != nil {
				return l
			}
		}
	}
	if l := loadZone(os.Getenv("PARATRACK_TZ")); l != nil {
		return l
	}
	return time.Local
}

// userNow is time.Now() in the user's zone (same instant).
func userNow(r *http.Request) time.Time { return time.Now().In(userLoc(r)) }
