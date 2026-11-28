package web

import (
	"sort"
	"testing"
	"time"
)

// The calendar of a year is a transcription of two sources: the Labour Code's
// holidays and the decree that moves the weekends around them. 2027 is decree
// 1187 of 17.09.2026 — 2 January to 5 November, 3 January to 31 December,
// 20 February to 22 February — with the working Saturday of 20 February and
// the three days before a holiday shortened by an hour.
func TestProductionCalendarOf2027MatchesTheDecree(t *testing.T) {
	want := map[string]string{
		"2027-01-01": "off", "2027-01-02": "off", "2027-01-03": "off", "2027-01-04": "off",
		"2027-01-05": "off", "2027-01-06": "off", "2027-01-07": "off", "2027-01-08": "off",
		"2027-02-22": "off", "2027-02-23": "off",
		"2027-03-08": "off",
		"2027-05-01": "off", "2027-05-03": "off", "2027-05-09": "off", "2027-05-10": "off",
		"2027-06-12": "off", "2027-06-14": "off",
		"2027-11-04": "off", "2027-11-05": "off",
		"2027-12-31": "off",
		"2027-02-20": "short", "2027-04-30": "short", "2027-06-11": "short", "2027-11-03": "short",
	}
	got := map[string]string{}
	for _, day := range ruProductionCalendar[2027] {
		got[day.day] = day.kind
	}
	var missing, extra []string
	for day, kind := range want {
		if got[day] != kind {
			missing = append(missing, day+"="+kind)
		}
	}
	for day, kind := range got {
		if _, ok := want[day]; !ok {
			extra = append(extra, day+"="+kind)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("the 2027 calendar differs from the decree: missing %v, extra %v", missing, extra)
	}
	// Every entry is a real day, and no day is listed twice: the set above is
	// the decree, a second entry for one day would say two things about it.
	seen := map[string]bool{}
	for _, entry := range ruProductionCalendar[2027] {
		if _, err := time.Parse("2006-01-02", entry.day); err != nil {
			t.Fatalf("%q is not a day: %v", entry.day, err)
		}
		if seen[entry.day] {
			t.Fatalf("%q is listed twice", entry.day)
		}
		seen[entry.day] = true
	}
}

// A working Saturday is a working day an hour shorter: marking it 'short' is
// what the workspace calendar means by it, and what makes the week's norm
// count it as a working day.
func TestWorkingSaturdayCountsAsAShortenedWorkingDay(t *testing.T) {
	var kinds []string
	for _, day := range ruProductionCalendar[2027] {
		if day.day == "2027-02-20" {
			kinds = append(kinds, day.kind)
		}
	}
	if len(kinds) != 1 || kinds[0] != "short" {
		t.Fatalf("20 February 2027 is %v, want one shortened working day", kinds)
	}
	years := ruCalendarYears()
	if len(years) < 2 || years[0] != 2026 || years[1] != 2027 {
		t.Fatalf("the year picker offers %v, want 2026 and 2027", years)
	}
}
