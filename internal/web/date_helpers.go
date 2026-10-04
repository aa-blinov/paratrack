package web

import (
	"net/http"
	"time"

	"github.com/aa-blinov/paratrack/internal/timeparse"
)

func (s *Server) parsePeriodAt(r *http.Request, now time.Time) timeparse.Period {
	name := r.URL.Query().Get("period")
	if name == "" {
		name = "today"
	}
	if name == "custom" {
		startStr := r.URL.Query().Get("start")
		endStr := r.URL.Query().Get("end")
		if startStr != "" && endStr != "" {
			if start, err := timeparse.ParseDateTime(startStr, now); err == nil {
				if end, err := timeparse.ParseDateTime(endStr, now); err == nil && end.After(start) {
					return timeparse.Period{Start: start, End: end, Label: "custom"}
				}
			}
		}
	}
	p, err := timeparse.ResolvePeriod(name, now)
	if err != nil {
		p, _ = timeparse.ResolvePeriod("today", now)
	}
	if weekStartsSunday(r) && (name == "week" || name == "last_week") {
		// ResolvePeriod counts from Monday; move to the Sunday-based week.
		ws := startOfWeek(r, now)
		if name == "week" {
			p.Start = ws
		} else {
			p.Start, p.End = ws.AddDate(0, 0, -7), ws.Add(-time.Second)
		}
	}
	return p
}

func startOfWeek(r *http.Request, t time.Time) time.Time {
	wd := (int(t.Weekday()) + 6) % 7 // Mon=0 … Sun=6
	if weekStartsSunday(r) {
		wd = int(t.Weekday())
	}
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return d.AddDate(0, 0, -wd)
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
