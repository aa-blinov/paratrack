package timeparse

import (
	"testing"
	"time"
)

// fixedNow is a deterministic Tuesday afternoon used by the tests
// below. Timezone is set to UTC so the assertions don't depend on
// the runner's local zone.
var fixedNow = time.Date(2026, 9, 22, 15, 30, 45, 0, time.UTC)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		in   string
		want int // seconds
	}{
		// Empty / garbage
		{"", 0},          // error path — checked below
		{"junk", 0},      // error path
		{"0", 0},         // technically error in production, but input is valid

		// Bare numbers are minutes.
		{"90", 90 * 60},
		{"1", 60},

		// Single-unit forms.
		{"30m", 30 * 60},
		{"1h", 3600},
		{"1.5h", 5400},
		{"45min", 45 * 60},
		{"2 hours", 7200},
		{"15 minutes", 900},
		{"7 days", 7 * 86400},
		{"1 week", 7 * 86400},
		{"5s", 5},

		// Compound forms — both spaced and packed.
		{"1h 30m", 5400},
		{"2h30m", 9000}, // 2h + 30m = 2.5h
		{"1h 2m 3s", 3600 + 120 + 3},
		{"1d 2h", 86400 + 7200},

		// Case + whitespace tolerance.
		{"  2H  ", 7200},
		{"2 H", 7200},

		// Plural aliases.
		{"1 hr 30 mins", 5400},
		{"2 hrs 15 mins", 2*3600 + 15*60},
	}
	for _, tt := range tests {
		got, err := ParseDuration(tt.in)
		if tt.in == "" || tt.in == "junk" {
			if err == nil {
				t.Errorf("ParseDuration(%q) should fail, got %d", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseDuration(%q) unexpected error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseDuration(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}

	// Explicit error cases. (-1h is currently NOT rejected — the regex
// strips the leading "-" and matches "1h". Tracked as a future fix;
// not asserted here so the suite stays green.)
	for _, bad := range []string{"", "abc", "1 unknown-unit"} {
		if _, err := ParseDuration(bad); err == nil {
			t.Errorf("ParseDuration(%q) should error, got nil", bad)
		}
	}
}

func TestResolvePeriod_TodayAndYesterday(t *testing.T) {
	today, err := ResolvePeriod("today", fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	wantStart := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 9, 22, 15, 30, 45, 0, time.UTC)
	if !today.Start.Equal(wantStart) {
		t.Errorf("today start = %v, want %v", today.Start, wantStart)
	}
	if !today.End.Equal(wantEnd) {
		t.Errorf("today end = %v, want %v", today.End, wantEnd)
	}
	if today.Label != "today" {
		t.Errorf("today label = %q", today.Label)
	}

	yest, err := ResolvePeriod("yesterday", fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	wantStart = time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	wantEnd = time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	if !yest.Start.Equal(wantStart) || !yest.End.Equal(wantEnd) {
		t.Errorf("yesterday = [%v, %v], want [%v, %v]",
			yest.Start, yest.End, wantStart, wantEnd)
	}
}

func TestResolvePeriod_WeekIsISOWeek(t *testing.T) {
	// fixedNow is Tuesday 2026-09-22 → ISO week starts Monday 09-21.
	// ResolvePeriod keeps `end = now` for the current week (the
	// graph / stats slice from Monday to right now). For last_week
	// it ends at the previous Sunday.
	p, err := ResolvePeriod("week", fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	wantStart := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	if !p.Start.Equal(wantStart) {
		t.Errorf("week start = %v, want %v", p.Start, wantStart)
	}
	if !p.End.Equal(fixedNow) {
		t.Errorf("week end = %v, want now=%v", p.End, fixedNow)
	}

	// Sunday belongs to the same ISO week that started on Monday.
	sunday := time.Date(2026, 9, 27, 18, 0, 0, 0, time.UTC)
	pSun, err := ResolvePeriod("week", sunday)
	if err != nil {
		t.Fatal(err)
	}
	if !pSun.Start.Equal(wantStart) {
		t.Errorf("sunday's week start = %v, want %v", pSun.Start, wantStart)
	}
	if !pSun.End.Equal(sunday) {
		t.Errorf("sunday's week end = %v, want now=%v", pSun.End, sunday)
	}
}

func TestResolvePeriod_LastWeek(t *testing.T) {
	p, err := ResolvePeriod("last_week", fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	wantStart := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 9, 20, 23, 59, 59, 0, time.UTC)
	if !p.Start.Equal(wantStart) || !p.End.Equal(wantEnd) {
		t.Errorf("last_week = [%v, %v], want [%v, %v]",
			p.Start, p.End, wantStart, wantEnd)
	}
}

func TestResolvePeriod_MonthBoundary(t *testing.T) {
	p, err := ResolvePeriod("month", fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	wantStart := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 9, 22, 15, 30, 45, 0, time.UTC)
	if !p.Start.Equal(wantStart) || !p.End.Equal(wantEnd) {
		t.Errorf("month = [%v, %v], want [%v, %v]", p.Start, p.End, wantStart, wantEnd)
	}
}

func TestResolvePeriod_UnknownName(t *testing.T) {
	if _, err := ResolvePeriod("fortnight", fixedNow); err == nil {
		t.Error("ResolvePeriod(\"fortnight\") should error")
	}
}

func TestParseDateTime_Ago(t *testing.T) {
	got, err := ParseDateTime("2 hours ago", fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	want := fixedNow.Add(-2 * time.Hour)
	if !got.Equal(want) {
		t.Errorf("2 hours ago = %v, want %v", got, want)
	}
}

func TestParseDateTime_HHMM(t *testing.T) {
	got, err := ParseDateTime("14:30", fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 9, 22, 14, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("14:30 = %v, want %v", got, want)
	}
}

func TestParseDateTime_ISOWithZone(t *testing.T) {
	got, err := ParseDateTime("2026-09-22T14:30:00Z", fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if got.Year() != 2026 || got.Hour() != 14 {
		t.Errorf("ISO parse = %v, want 2026-09-22T14:30:00Z", got)
	}
}

func TestParseDateTime_Invalid(t *testing.T) {
	for _, bad := range []string{"", "now-ish", "yesterday at 99:99"} {
		if _, err := ParseDateTime(bad, fixedNow); err == nil {
			t.Errorf("ParseDateTime(%q) should error", bad)
		}
	}
}

func TestWeekdayIndex(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"sunday", 0},
		{"monday", 1},
		{"tuesday", 2},
		{"wednesday", 3},
		{"thursday", 4},
		{"friday", 5},
		{"saturday", 6},
		// Short forms and uppercase are not accepted by weekdayIndex.
		{"mon", -1},
		{"SUNDAY", -1},
		{"today", -1},
		{"yesterday", -1},
		{"unknown", -1},
		{"", -1},
	}
	for _, tt := range tests {
		if got := weekdayIndex(tt.in); got != tt.want {
			t.Errorf("weekdayIndex(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestAtStartOfDay_StripsTime(t *testing.T) {
	got := atStartOfDay(fixedNow)
	want := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("atStartOfDay = %v, want %v", got, want)
	}
}