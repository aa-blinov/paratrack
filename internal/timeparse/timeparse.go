// Package timeparse converts natural-language datetime and duration strings
// into Go time.Time / int values, matching the ergonomics of the original
// Python CLI (which used dateparser). Only a focused subset is supported —
// enough for a single-user time tracker.
//
// Supported datetime forms:
//
//	"now"                                  current moment
//	"today"                                today at 00:00
//	"yesterday"                            yesterday at 00:00
//	"tomorrow"                             tomorrow at 00:00
//	"YYYY-MM-DD"                           date at 00:00
//	"YYYY-MM-DD HH:MM[:SS]"                date + time
//	"YYYY-MM-DDTHH:MM[:SS][Z|+HH:MM]"      ISO 8601
//	"HH:MM"                                today at HH:MM
//	"yesterday HH:MM"                      yesterday at HH:MM
//	"N hours ago", "N min ago", "N days ago"  past relative
//	"monday".. "sunday"                    most-recent past occurrence
//	"last monday".. "last sunday"           strictly the previous one
//
// Supported duration forms:
//
//	"90"               90 minutes
//	"1h", "1.5h"       hours
//	"30m", "30 min"    minutes
//	"90s"              seconds
//	"1h 30m", "2h30m"  compound
//	"1 hour", "30 minutes"  long forms
package timeparse

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ParseDateTime parses s relative to now. Local zone is used for
// bare dates; explicit offsets/Z are honoured.
func ParseDateTime(s string, now time.Time) (time.Time, error) {
	orig := strings.TrimSpace(s)
	low := strings.ToLower(orig)
	s = low
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time string")
	}

	// "now" — short circuit so the table below doesn't have to.
	if s == "now" {
		return now, nil
	}

	// Bare keywords.
	switch s {
	case "today":
		return atStartOfDay(now), nil
	case "yesterday":
		return atStartOfDay(now).AddDate(0, 0, -1), nil
	case "tomorrow":
		return atStartOfDay(now).AddDate(0, 0, 1), nil
	}

	// "N (units) ago"
	if t, ok := parseAgo(s, now); ok {
		return t, nil
	}

	// "last <weekday>" / "<weekday>" / "<weekday> HH:MM"
	if t, ok := parseWeekdayOrWeekdayTime(s, now); ok {
		return t, nil
	}

	// "yesterday HH:MM" / "tomorrow HH:MM" / "today HH:MM"
	for _, prefix := range []string{"yesterday ", "tomorrow ", "today "} {
		if strings.HasPrefix(s, prefix) {
			hhmm := strings.TrimPrefix(s, prefix)
			t, err := parseHHMM(hhmm)
			if err != nil {
				return time.Time{}, err
			}
			base := atStartOfDay(now)
			switch prefix[:len(prefix)-1] {
			case "yesterday":
				base = base.AddDate(0, 0, -1)
			case "tomorrow":
				base = base.AddDate(0, 0, 1)
			}
			return base.Add(time.Duration(t.h)*time.Hour + time.Duration(t.m)*time.Minute), nil
		}
	}

	// Bare HH:MM → today
	if strings.Contains(s, ":") && !strings.Contains(s, "-") && !strings.Contains(s, "t") {
		if t, err := parseHHMM(s); err == nil {
			return atStartOfDay(now).Add(time.Duration(t.h)*time.Hour + time.Duration(t.m)*time.Minute), nil
		}
	}

	// Explicit datetimes. Try several layouts.
	candidate := orig
	if candidate == "" {
		candidate = s
	}
	for _, layout := range []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
		"02.01.2006",
		"02/01/2006",
	} {
		if t, err := time.ParseInLocation(layout, candidate, time.Local); err == nil {
			return t, nil
		}
		if t, err := time.Parse(layout, candidate); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("cannot parse time %q", s)
}

// ParseDuration returns the total seconds represented by s, or an error.
// Bare numbers default to minutes — same quirk as the Python tracker,
// because typing "90" is so common for "90 minutes".
func ParseDuration(s string) (int, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	// Refuse negative durations up front. The regex below would
	// otherwise strip the leading '-' and quietly parse "1h" out of
	// "-1h", returning a positive value.
	if strings.HasPrefix(s, "-") {
		return 0, fmt.Errorf("duration must be positive: %q", s)
	}
	// Bare number → minutes
	if n, err := strconv.Atoi(s); err == nil {
		return n * 60, nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && !strings.ContainsAny(s, "hm") {
		// Float with no unit is still minutes (covers "1.5" → 1.5 min)
		return int(f * 60), nil
	}

	// Tokenise: number + unit, possibly many. Spaces optional between
	// tokens (so "2h30m" and "1h 30m" both work). The unit alternative
	// uses regex alternation that never matches letters beyond itself,
	// which lets adjacent tokens like "2h30m" split cleanly at the digit
	// boundary.
	tokens := durationTokenRe.FindAllStringSubmatch(s, -1)
	if len(tokens) == 0 {
		return 0, fmt.Errorf("cannot parse duration %q", s)
	}
	total := 0.0
	for _, tok := range tokens {
		val, err := strconv.ParseFloat(tok[1], 64)
		if err != nil {
			return 0, fmt.Errorf("bad number in %q", tok[0])
		}
		unit := tok[2]
		switch unit {
		case "h", "hr", "hrs", "hour", "hours":
			total += val * 3600
		case "m", "min", "mins", "minute", "minutes":
			total += val * 60
		case "s", "sec", "secs", "second", "seconds":
			total += val
		case "d", "day", "days":
			total += val * 86400
		case "w", "week", "weeks":
			total += val * 86400 * 7
		default:
			return 0, fmt.Errorf("unknown unit %q in duration", unit)
		}
	}
	if total <= 0 {
		return 0, fmt.Errorf("duration must be positive: %q", s)
	}
	return int(total), nil
}

// Period is a closed-open date range used by log/stats.
type Period struct {
	Start time.Time
	End   time.Time
	Label string
}

// ResolvePeriod maps a short name ("today", "yesterday", "week", "month",
// "last_week", "last_month") to a Period. "custom" requires start/end
// arguments; if missing, returns an error.
func ResolvePeriod(name string, now time.Time) (Period, error) {
	startOfDay := func(t time.Time) time.Time {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	}
	start := time.Time{}
	end := now
	switch name {
	case "today":
		start = startOfDay(now)
	case "yesterday":
		end = startOfDay(now)
		start = end.AddDate(0, 0, -1)
	case "week":
		// Monday as start of week.
		offset := int(now.Weekday()) - int(time.Monday)
		if offset < 0 {
			offset += 7
		}
		start = startOfDay(now.AddDate(0, 0, -offset))
	case "last_week":
		offset := int(now.Weekday()) - int(time.Monday)
		if offset < 0 {
			offset += 7
		}
		thisMon := startOfDay(now.AddDate(0, 0, -offset))
		end = thisMon.Add(-time.Second) // Sunday 23:59:59
		start = thisMon.AddDate(0, 0, -7)
	case "month":
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	case "last_month":
		firstThis := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		end = firstThis.Add(-time.Second)
		start = time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, end.Location())
	default:
		return Period{}, fmt.Errorf("unknown period %q", name)
	}
	return Period{Start: start, End: end, Label: name}, nil
}

// ---- internals ------------------------------------------------------

type hhmm struct{ h, m int }

func parseHHMM(s string) (hhmm, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return hhmm{}, fmt.Errorf("expected HH:MM, got %q", s)
	}
	h, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || h < 0 || h > 23 {
		return hhmm{}, fmt.Errorf("bad hour in %q", s)
	}
	m, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || m < 0 || m > 59 {
		return hhmm{}, fmt.Errorf("bad minute in %q", s)
	}
	return hhmm{h, m}, nil
}

var (
	agoRe     = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s+(hour|hours|hr|hrs|minute|minutes|min|mins|day|days|week|weeks)\s+ago$`)
	// durationTokenRe matches number + unit word; the unit alternative
	// is bounded (no \b) so that "2h30m" splits at "2h" + "30m".
	durationTokenRe = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*(h|hrs?|hours?|m|mins?|minutes?|s|secs?|seconds?|d|days?|w|weeks?)`)
	// weekdayRe matches bare "monday" / "last monday" / "<weekday> HH:MM" / "last <weekday> HH:MM".
	weekdayRe = regexp.MustCompile(`^(last\s+)?(monday|tuesday|wednesday|thursday|friday|saturday|sunday)(\s+\d{1,2}:\d{2})?$`)
)

func parseAgo(s string, now time.Time) (time.Time, bool) {
	m := agoRe.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return time.Time{}, false
	}
	var d time.Duration
	switch m[2] {
	case "hour", "hours", "hr", "hrs":
		d = time.Duration(val * float64(time.Hour))
	case "minute", "minutes", "min", "mins":
		d = time.Duration(val * float64(time.Minute))
	case "day", "days":
		d = time.Duration(val * float64(time.Hour) * 24)
	case "week", "weeks":
		d = time.Duration(val * float64(time.Hour) * 24 * 7)
	default:
		return time.Time{}, false
	}
	return now.Add(-d), true
}

func parseWeekdayOrWeekdayTime(s string, now time.Time) (time.Time, bool) {
	m := weekdayRe.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	day, ok := resolveWeekday(m, now)
	if !ok {
		return time.Time{}, false
	}
	if m[3] != "" {
		// Trailing " HH:MM" — adjust the returned day.
		t, err := parseHHMM(strings.TrimSpace(m[3]))
		if err != nil {
			return time.Time{}, false
		}
		day = day.Add(time.Duration(t.h)*time.Hour + time.Duration(t.m)*time.Minute)
	}
	return day, true
}

func resolveWeekday(m []string, now time.Time) (time.Time, bool) {
	target := weekdayIndex(m[2])
	wantLast := m[1] != ""
	today := atStartOfDay(now)
	for offset := 0; offset < 14; offset++ {
		d := today.AddDate(0, 0, -offset)
		if int(d.Weekday()) == target {
			if wantLast && offset < 7 {
				continue
			}
			return d, true
		}
	}
	return time.Time{}, false
}

func weekdayIndex(name string) int {
	switch name {
	case "sunday":
		return 0
	case "monday":
		return 1
	case "tuesday":
		return 2
	case "wednesday":
		return 3
	case "thursday":
		return 4
	case "friday":
		return 5
	case "saturday":
		return 6
	}
	return -1
}

func atStartOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
