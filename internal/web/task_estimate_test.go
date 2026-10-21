package web

import "testing"

// The task estimate is entered as hours and minutes, like a session. Plain
// minutes still parse, so older clients keep working.
func TestTaskEstimateReadsClockAndMinutes(t *testing.T) {
	cases := map[string]int{
		"":       0,
		"0":      0,
		"90":     90,
		"1:30":   90,
		"8:00":   480,
		"1h 30m": 90,
	}
	for raw, want := range cases {
		got, err := taskEstimateMinutes(raw)
		if err != nil {
			t.Errorf("taskEstimateMinutes(%q) error: %v", raw, err)
			continue
		}
		if got != want {
			t.Errorf("taskEstimateMinutes(%q) = %d, want %d", raw, got, want)
		}
	}
	for _, bad := range []string{"abc", "1:60", "1:5", "-1:00"} {
		if got, err := taskEstimateMinutes(bad); err == nil {
			t.Errorf("taskEstimateMinutes(%q) = %d, want an error", bad, got)
		}
	}
}
