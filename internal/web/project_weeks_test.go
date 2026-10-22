package web

import (
	"testing"
	"time"
)

// A week is off its norm when it strays by more than a tenth of it, in either
// direction (PLAN-02). A week a little short is normal.
func TestWeekOffMarksOnlyAWeekFarFromTheNorm(t *testing.T) {
	norm := 30 * 3600
	cases := []struct {
		figure int
		off    bool
	}{
		{norm, false},
		{27 * 3600, false}, // exactly a tenth short
		{26 * 3600, true},
		{34 * 3600, true},
		{0, true},
	}
	for _, c := range cases {
		if got := weekOff(c.figure, norm); got != c.off {
			t.Errorf("weekOff(%d h, 30 h) = %v, want %v", c.figure/3600, got, c.off)
		}
	}
	if weekOff(5*3600, 0) {
		t.Error("a project without a norm marks no week")
	}
}

// The weeks around today are the Mondays of those weeks, nothing else.
func TestProjectWeeksAreMondays(t *testing.T) {
	now := time.Date(2026, 10, 22, 15, 0, 0, 0, time.UTC) // a Thursday
	weeks := projectWeeks(now, 3, 2)
	if len(weeks) != 6 {
		t.Fatalf("got %d weeks, want 6", len(weeks))
	}
	for _, week := range weeks {
		if week.Weekday() != time.Monday {
			t.Errorf("week %s starts on %s, want a Monday", week.Format("2006-01-02"), week.Weekday())
		}
	}
	if got := weeks[3].Format("2006-01-02"); got != "2026-10-19" {
		t.Errorf("the current week starts %s, want 2026-10-19", got)
	}
	if got := weeks[5].Format("2006-01-02"); got != "2026-11-02" {
		t.Errorf("the last week starts %s, want 2026-11-02", got)
	}
}
