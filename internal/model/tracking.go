package model

import (
	"time"
)

// MaxSessionDurationSeconds is the largest session duration that can safely
// convert to time.Duration nanoseconds.
const MaxSessionDurationSeconds int64 = (1<<63 - 1) / int64(time.Second)

type Session struct {
	ID                 int64
	ActivityID         int64
	TeamID             int64
	UserID             int64
	StartAt            time.Time
	EndAt              *time.Time
	Note               *string
	Paused             bool
	PausedAt           *time.Time
	AccumulatedSeconds int
	LastResumeAt       *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func maxSessionSecondsInt() int64 {
	maxSeconds := MaxSessionDurationSeconds
	maxInt := int64(int(^uint(0) >> 1))
	if maxInt < maxSeconds {
		maxSeconds = maxInt
	}
	return maxSeconds
}

func clampSessionSeconds(seconds int64) int {
	if seconds <= 0 {
		return 0
	}
	if max := maxSessionSecondsInt(); seconds > max {
		return int(max)
	}
	return int(seconds)
}

func addSessionSecondsSaturated(left, right int) int {
	maxSeconds := int(maxSessionSecondsInt())
	if left < 0 {
		left = 0
	}
	if right < 0 {
		right = 0
	}
	if right > maxSeconds-left {
		return maxSeconds
	}
	return left + right
}

// SessionInvoiceLockError carries the invoice that prevents a session edit.
type SessionInvoiceLockError struct{ InvoiceNumber string }

func (e *SessionInvoiceLockError) Error() string { return ErrSessionInvoiceLocked.Error() }

func (e *SessionInvoiceLockError) Unwrap() error { return ErrSessionInvoiceLocked }

// DurationSeconds returns the tracked (non-paused) seconds for a
// session. For active sessions, live time since last resume is added.
// For closed sessions this is accumulated_seconds — which UpdateSessionEnd
// now maintains — falling back to the wall-clock span for legacy rows
// that stopped before that fold existed (accumulated left at 0).
func (s Session) DurationSeconds(now time.Time) int {
	if s.EndAt != nil {
		if s.AccumulatedSeconds > 0 {
			return clampSessionSeconds(int64(s.AccumulatedSeconds))
		}
		// Legacy closed row: never paused, so span == tracked.
		return clampSessionSeconds(int64(s.EndAt.Sub(s.StartAt) / time.Second))
	}
	total := s.AccumulatedSeconds
	if total < 0 {
		total = 0
	}
	if !s.Paused && s.LastResumeAt != nil {
		live := int(safeOverlapDurationSeconds(*s.LastResumeAt, now))
		total = addSessionSecondsSaturated(total, live)
	}
	return clampSessionSeconds(int64(total))
}

// TrackedSecondsInWindow returns the non-paused seconds attributable to
// [winStart, winEnd]. The session's tracked total is scaled by how much
// of its wall-clock span overlaps the window, so a session straddling
// midnight (or a pause) doesn't dump all of its time into one day.
//
// An open session splits in two: the live stretch since the last resume
// is tracked second for second, so it lands exactly in its window; only
// the accumulated part is scaled, over the span that produced it (start
// to last resume, or to the pause). Scaling the whole open span instead
// grew "today" by a fraction of a second per second for a timer started
// yesterday, and grew it for a paused timer that was not running at all.
func (s Session) TrackedSecondsInWindow(winStart, winEnd, now time.Time) int {
	if s.EndAt == nil {
		live := 0
		accEnd := now
		if s.Paused {
			if s.PausedAt != nil {
				accEnd = *s.PausedAt
			}
		} else if s.LastResumeAt != nil {
			accEnd = *s.LastResumeAt
			live = overlapSeconds(*s.LastResumeAt, now, winStart, winEnd)
		}
		acc := Session{StartAt: s.StartAt, EndAt: &accEnd, AccumulatedSeconds: s.AccumulatedSeconds}
		if s.AccumulatedSeconds <= 0 {
			return live
		}
		return addSessionSecondsSaturated(acc.TrackedSecondsInWindow(winStart, winEnd, now), live)
	}
	spanStart := s.StartAt
	spanEnd := *s.EndAt
	if !spanEnd.After(spanStart) {
		return 0
	}
	// Overlap of the wall-clock span with the window.
	ovStart, ovEnd := spanStart, spanEnd
	if ovStart.Before(winStart) {
		ovStart = winStart
	}
	if ovEnd.After(winEnd) {
		ovEnd = winEnd
	}
	if !ovEnd.After(ovStart) {
		return 0
	}
	wall := spanEnd.Sub(spanStart).Seconds()
	overlap := ovEnd.Sub(ovStart).Seconds()
	tracked := s.DurationSeconds(now)
	// Scale tracked time by the overlapping fraction of the span.
	scaledValue := float64(tracked) * overlap / wall
	if scaledValue >= float64(maxSessionSecondsInt()) {
		return int(maxSessionSecondsInt())
	}
	scaled := int(scaledValue)
	// Round sub-second overlaps up so a just-started session is visible.
	if scaled <= 0 && overlap > 0 {
		scaled = 1
	}
	return scaled
}

// overlapSeconds is the length of [a, b] ∩ [winStart, winEnd] in seconds.
func overlapSeconds(a, b, winStart, winEnd time.Time) int {
	if a.Before(winStart) {
		a = winStart
	}
	if b.After(winEnd) {
		b = winEnd
	}
	if !b.After(a) {
		return 0
	}
	return int(safeOverlapDurationSeconds(a, b))
}

func safeOverlapDurationSeconds(start, end time.Time) int64 {
	if !end.After(start) {
		return 0
	}
	seconds := int64(end.Sub(start) / time.Second)
	if seconds > maxSessionSecondsInt() {
		return maxSessionSecondsInt()
	}
	return seconds
}

// Active reports whether the session is open and unfinished.
func (s Session) Active() bool { return s.EndAt == nil }

// ActiveSession pairs a session with its activity for display.
type ActiveSession struct {
	Session  Session
	Activity Activity
}
