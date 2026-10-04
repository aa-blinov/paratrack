package model

import "time"

// SessionIntervalEdit is the interval-specific intent of a session edit.
// Nil timestamps leave their stored values unchanged.
type SessionIntervalEdit struct {
	StartAt            *time.Time
	EndAt              *time.Time
	AccumulatedSeconds *int
	DurationSeconds    *int
	RecomputeDuration  bool
}

// ResolvedSessionInterval contains only the fields that persistence should
// write after resolving an edit against the session's current interval.
type ResolvedSessionInterval struct {
	StartAt            *time.Time
	EndAt              *time.Time
	AccumulatedSeconds *int
}

// ResolveSessionIntervalEdit applies interval rules to the locked session
// state. It is pure so the persistence adapter can call it while holding its
// row lock without owning the domain rules itself.
func ResolveSessionIntervalEdit(currentStart time.Time, currentEnd *time.Time, edit SessionIntervalEdit) (ResolvedSessionInterval, error) {
	resolved := ResolvedSessionInterval{
		StartAt: edit.StartAt, EndAt: edit.EndAt,
		AccumulatedSeconds: edit.AccumulatedSeconds,
	}
	start := currentStart
	if edit.StartAt != nil {
		start = *edit.StartAt
	}
	var end *time.Time
	if currentEnd != nil {
		current := *currentEnd
		end = &current
	}
	if edit.EndAt != nil {
		end = edit.EndAt
	}
	if edit.DurationSeconds != nil {
		seconds := *edit.DurationSeconds
		if seconds < 0 {
			return ResolvedSessionInterval{}, ErrSessionLengthInvalid
		}
		if int64(seconds) > MaxSessionDurationSeconds {
			return ResolvedSessionInterval{}, ErrSessionDurationOverflow
		}
		newEnd := start.Add(time.Duration(seconds) * time.Second)
		resolved.EndAt, resolved.AccumulatedSeconds = &newEnd, &seconds
		end = resolved.EndAt
	} else if edit.RecomputeDuration && end != nil {
		seconds := int(end.Sub(start).Seconds())
		resolved.AccumulatedSeconds = &seconds
	}
	if (edit.StartAt != nil || edit.EndAt != nil || edit.DurationSeconds != nil) && end != nil {
		if !end.After(start) {
			return ResolvedSessionInterval{}, ErrSessionPeriodInvalid
		}
		if end.Sub(start) > time.Duration(MaxSessionDurationSeconds)*time.Second {
			return ResolvedSessionInterval{}, ErrSessionDurationOverflow
		}
	}
	if resolved.AccumulatedSeconds != nil {
		seconds := *resolved.AccumulatedSeconds
		if seconds < 0 {
			return ResolvedSessionInterval{}, ErrSessionLengthInvalid
		}
		if int64(seconds) > MaxSessionDurationSeconds {
			return ResolvedSessionInterval{}, ErrSessionDurationOverflow
		}
	}
	return resolved, nil
}
