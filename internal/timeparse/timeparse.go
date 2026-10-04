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
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

var ErrDurationOverflow = errors.New("duration exceeds representable range")

// maxDurationSeconds keeps parsed values safe both as machine ints and when
// callers convert them to time.Duration nanoseconds.
func maxDurationSeconds() int {
	maxInt := int64(int(^uint(0) >> 1))
	maxSeconds := model.MaxSessionDurationSeconds
	if maxInt < maxSeconds {
		maxSeconds = maxInt
	}
	return int(maxSeconds)
}

func durationSeconds(seconds float64) (int, error) {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds > float64(maxDurationSeconds()) {
		return 0, ErrDurationOverflow
	}
	return int(seconds), nil
}

// ParseDateTime parses s relative to now. Local zone is used for
// bare dates; explicit offsets/Z are honoured.
func ParseDateTime(s string, now time.Time) (time.Time, error) {
	orig := strings.TrimSpace(s)
	low := strings.ToLower(orig)
	s = low
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time string")
	}

	// Russian keywords map onto the English grammar below.
	s = ruKeywords.Replace(s)
	for _, suffix := range []string{" назад", " ago"} {
		if rest, ok := strings.CutSuffix(s, suffix); ok {
			if d, err := ParseDuration(rest); err == nil {
				return now.Add(-time.Duration(d) * time.Second), nil
			}
		}
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

	// Explicit datetimes. Naive layouts use the location carried by the
	// caller's clock so configured process timezones apply consistently.
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
		if t, err := time.ParseInLocation(layout, candidate, now.Location()); err == nil {
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
	s = strings.TrimSpace(strings.ToLower(strings.ReplaceAll(s, "\u00a0", " ")))
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
		if n > maxDurationSeconds()/60 {
			return 0, ErrDurationOverflow
		}
		return n * 60, nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && !strings.ContainsAny(s, "hmчм") {
		// Float with no unit is still minutes (covers "1.5" → 1.5 min).
		return durationSeconds(f * 60)
	} else if errors.Is(err, strconv.ErrRange) && !strings.ContainsAny(s, "hmчм") {
		return 0, ErrDurationOverflow
	}

	// Tokenise: number + unit, possibly many. Spaces optional between
	// tokens (so "2h30m" and "1h 30m" both work). The unit alternative
	// uses regex alternation that never matches letters beyond itself,
	// which lets adjacent tokens like "2h30m" split cleanly at the digit
	// boundary.
	tokens := durationTokenRe.FindAllStringSubmatchIndex(s, -1)
	if len(tokens) == 0 {
		return 0, fmt.Errorf("cannot parse duration %q", s)
	}
	total := 0.0
	cursor := 0
	for _, token := range tokens {
		if strings.TrimSpace(s[cursor:token[0]]) != "" {
			return 0, fmt.Errorf("cannot parse duration %q", s)
		}
		val, err := strconv.ParseFloat(s[token[2]:token[3]], 64)
		if err != nil {
			return 0, fmt.Errorf("bad number in %q", s[token[0]:token[1]])
		}
		unit := s[token[4]:token[5]]
		var multiplier float64
		switch unit {
		case "h", "hr", "hrs", "hour", "hours", "ч", "час", "часа", "часов":
			multiplier = 3600
		case "m", "min", "mins", "minute", "minutes", "м", "мин", "минута", "минуты", "минут":
			multiplier = 60
		case "s", "sec", "secs", "second", "seconds", "с", "сек":
			multiplier = 1
		case "d", "day", "days", "д", "дн", "день", "дня", "дней":
			multiplier = 86400
		case "w", "week", "weeks", "н", "нед", "неделя", "недели", "недель":
			multiplier = 86400 * 7
		default:
			return 0, fmt.Errorf("unknown unit %q in duration", unit)
		}
		total += val * multiplier
		if math.IsInf(total, 0) || total > float64(maxDurationSeconds()) {
			return 0, ErrDurationOverflow
		}
		cursor = token[1]
	}
	if strings.TrimSpace(s[cursor:]) != "" {
		return 0, fmt.Errorf("cannot parse duration %q", s)
	}
	if total <= 0 {
		return 0, fmt.Errorf("duration must be positive: %q", s)
	}
	return durationSeconds(total)
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
func IsKnownPeriod(name string) bool {
	switch name {
	case "today", "yesterday", "week", "last_week", "month", "last_month":
		return true
	default:
		return false
	}
}

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
	agoRe = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s+(hour|hours|hr|hrs|minute|minutes|min|mins|day|days|week|weeks)\s+ago$`)
	// durationTokenRe matches number + unit word; the unit alternative
	// is bounded (no \b) so that "2h30m" splits at "2h" + "30m".
	durationTokenRe = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*(hours?|hrs?|h|minutes?|mins?|m|seconds?|secs?|s|days?|d|weeks?|w|часа|часов|час|ч|минуты|минута|минут|мин|м|сек|с|дней|дня|день|дн|д|недели|неделя|недель|нед|н)`)
	// weekdayRe matches bare "monday" / "last monday" / "<weekday> HH:MM" / "last <weekday> HH:MM".
	weekdayRe = regexp.MustCompile(`^(last\s+)?(monday|tuesday|wednesday|thursday|friday|saturday|sunday)(\s+\d{1,2}:\d{2})?$`)
)

var ruKeywords = strings.NewReplacer("сейчас", "now", "сегодня", "today", "вчера", "yesterday", "завтра", "tomorrow")

func parseAgo(s string, now time.Time) (time.Time, bool) {
	m := agoRe.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return time.Time{}, false
	}
	var unit time.Duration
	switch m[2] {
	case "hour", "hours", "hr", "hrs":
		unit = time.Hour
	case "minute", "minutes", "min", "mins":
		unit = time.Minute
	case "day", "days":
		unit = 24 * time.Hour
	case "week", "weeks":
		unit = 7 * 24 * time.Hour
	default:
		return time.Time{}, false
	}
	nanoseconds := val * float64(unit)
	// float64(MaxInt64) rounds to 2^63, so the inclusive comparison
	// prevents converting that value to a negative time.Duration.
	const maxInt64 = int64(^uint64(0) >> 1)
	if math.IsNaN(nanoseconds) || math.IsInf(nanoseconds, 0) || nanoseconds >= float64(maxInt64) {
		return time.Time{}, false
	}
	d := time.Duration(nanoseconds)
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
