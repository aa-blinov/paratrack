package timeparse

import (
	"testing"
	"time"
)

// Dates are written the way people in Russian write them: dotted with a time,
// or a day with a short or long month name (TIME-10).
func TestRussianDatesParse(t *testing.T) {
	now := time.Date(2026, 10, 22, 15, 0, 0, 0, time.UTC)
	cases := map[string]time.Time{
		"13.10.2026 10:00":  time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC),
		"13.10.2026":        time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC),
		"13 окт 10:00":      time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC),
		"13 окт. 2026 9:30": time.Date(2026, 10, 13, 9, 30, 0, 0, time.UTC),
		"13 октября 10:00":  time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC),
		"5 мая 2026 18:15":  time.Date(2026, 5, 5, 18, 15, 0, 0, time.UTC),
	}
	for input, want := range cases {
		got, err := ParseDateTime(input, now)
		if err != nil {
			t.Errorf("%q: %v", input, err)
			continue
		}
		if !got.Equal(want) {
			t.Errorf("%q = %v, want %v", input, got, want)
		}
	}
}

// A day that does not exist is refused, not moved to the next month.
func TestRussianDateRefusesAnImpossibleDay(t *testing.T) {
	now := time.Date(2026, 10, 22, 15, 0, 0, 0, time.UTC)
	for _, input := range []string{"31 фев 10:00", "32.10.2026 10:00"} {
		if _, err := ParseDateTime(input, now); err == nil {
			t.Errorf("%q was accepted", input)
		}
	}
}
