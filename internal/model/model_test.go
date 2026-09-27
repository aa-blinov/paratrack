package model

import (
	"testing"
	"time"
)

// A timer started yesterday and resumed today counts today second for
// second; a paused one stops growing.
func TestTrackedSecondsInWindowOpenSession(t *testing.T) {
	day := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	end := day.Add(24 * time.Hour)
	start := day.Add(-2 * time.Hour) // yesterday 22:00
	resume := day.Add(1 * time.Hour) // today 01:00, 1h accumulated before
	now := day.Add(3 * time.Hour)
	s := Session{StartAt: start, AccumulatedSeconds: 3600, LastResumeAt: &resume}
	// acc 3600 over 22:00..01:00 (3h, 1h of it today) → 1200; live 2h → 7200.
	if got := s.TrackedSecondsInWindow(day, end, now); got != 8400 {
		t.Errorf("running: got %d, want 8400", got)
	}
	if got := s.TrackedSecondsInWindow(day, end, now.Add(time.Minute)); got != 8460 {
		t.Errorf("running must grow 60 s per minute: got %d", got)
	}
	paused := day.Add(2 * time.Hour)
	p := Session{StartAt: start, AccumulatedSeconds: 3600, Paused: true, PausedAt: &paused}
	a := p.TrackedSecondsInWindow(day, end, now)
	b := p.TrackedSecondsInWindow(day, end, now.Add(time.Hour))
	if a != b {
		t.Errorf("paused session must not grow: %d then %d", a, b)
	}
}
