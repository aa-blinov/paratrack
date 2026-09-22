package web

import (
	"testing"
)

// parseHMSStrict + filterByTag are the only non-template helpers in
// the web package that are easy to exercise without booting the
// server or a DB. They're also the kind of code that silently rots:
// change Duration's format string or rename a field and tests are the
// only thing that catches the regression.

func TestParseHMSStrict(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"00:00:00", 0},
		{"00:01:00", 60},
		{"00:00:30", 30},
		{"01:00:00", 3600},
		{"23:59:59", 86399},
		{"1:02:03", 3723},    // single-digit hour should still parse
		{"", 0},               // empty stays 0
		{"junk", 0},           // garbage stays 0, doesn't panic
		{"12:34", 0},          // missing seconds segment treated as 0
	}
	for _, tt := range tests {
		got := parseHMSStrict(tt.in)
		if got != tt.want {
			t.Errorf("parseHMSStrict(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// makeRow is a tiny builder so the table-driven test below reads as
// pure data instead of constructor noise.
func makeRow(id int64, activity, duration string, tagNames ...string) sessionView {
	tags := make([]tagChip, len(tagNames))
	for i, n := range tagNames {
		tags[i] = tagChip{ID: int64(i + 1), Name: n}
	}
	return sessionView{
		ID:           id,
		ActivityName: activity,
		Duration:     duration,
		Tags:         tags,
	}
}

func TestFilterByTag_OnlyMatchingRowsPass(t *testing.T) {
	rows := []sessionView{
		makeRow(1, "writing", "01:00:00", "deep-work"),
		makeRow(2, "reading", "00:30:00", "morning"),
		makeRow(3, "writing", "02:00:00", "deep-work", "morning"),
		makeRow(4, "work", "00:15:00"), // no tags
	}
	agg := map[string]int{
		"writing": 3 * 3600,
		"reading": 1800,
		"work":    900,
	}
	total := 3*3600 + 1800 + 900

	filtered, newAgg, newTotal := filterByTag(rows, agg, total, "deep-work")

	if len(filtered) != 2 {
		t.Fatalf("filtered len = %d, want 2", len(filtered))
	}
	// Order should be preserved (in-place skip).
	if filtered[0].ID != 1 || filtered[1].ID != 3 {
		t.Errorf("filtered rows = [%d, %d], want [1, 3]", filtered[0].ID, filtered[1].ID)
	}
	if newAgg["writing"] != 3*3600 || newAgg["reading"] != 0 || newAgg["work"] != 0 {
		t.Errorf("newAgg = %v, want only writing with full time", newAgg)
	}
	if newTotal != 3*3600 {
		t.Errorf("newTotal = %d, want %d", newTotal, 3*3600)
	}
}

func TestFilterByTag_NoMatch(t *testing.T) {
	rows := []sessionView{
		makeRow(1, "writing", "01:00:00", "evening"),
		makeRow(2, "reading", "00:30:00"),
	}
	agg := map[string]int{"writing": 3600, "reading": 1800}
	total := 5400

	filtered, newAgg, newTotal := filterByTag(rows, agg, total, "nope")

	if len(filtered) != 0 {
		t.Errorf("filtered should be empty, got %d rows", len(filtered))
	}
	if len(newAgg) != 0 || newTotal != 0 {
		t.Errorf("aggregate should be empty, got agg=%v total=%d", newAgg, newTotal)
	}
}

func TestFilterByTag_EmptyInput(t *testing.T) {
	filtered, newAgg, newTotal := filterByTag(nil, map[string]int{}, 0, "anything")
	if filtered != nil && len(filtered) != 0 {
		t.Errorf("empty rows should pass through, got %v", filtered)
	}
	if newTotal != 0 || len(newAgg) != 0 {
		t.Errorf("empty input should yield empty aggregate, got %v / %d", newAgg, newTotal)
	}
}

func TestFilterByTag_EmptyTagName(t *testing.T) {
	// An empty string in Tags.Name means the row carries no tags —
	// but the *filter* name itself can also be empty. With an empty
	// filter name, no row matches and the result is empty.
	rows := []sessionView{
		makeRow(1, "writing", "01:00:00", "deep-work"),
	}
	filtered, _, _ := filterByTag(rows, map[string]int{"writing": 3600}, 3600, "")
	if len(filtered) != 0 {
		t.Errorf("empty filter name should not match anything, got %d rows", len(filtered))
	}
}

func TestFilterByTag_PreservesOriginalOrder(t *testing.T) {
	rows := []sessionView{
		makeRow(1, "writing", "00:10:00", "x"),
		makeRow(2, "writing", "00:20:00", "y"),
		makeRow(3, "writing", "00:30:00", "x"),
		makeRow(4, "writing", "00:40:00", "y"),
		makeRow(5, "writing", "00:50:00", "x"),
	}
	filtered, _, _ := filterByTag(rows, map[string]int{"writing": 9000}, 9000, "x")
	want := []int64{1, 3, 5}
	if len(filtered) != len(want) {
		t.Fatalf("len = %d, want %d", len(filtered), len(want))
	}
	for i, id := range want {
		if filtered[i].ID != id {
			t.Errorf("filtered[%d].ID = %d, want %d", i, filtered[i].ID, id)
		}
	}
}

func TestFormatMinutes(t *testing.T) {
	tests := []struct {
		min  int
		want string
	}{
		{0, "0m"},
		{1, "1m"},
		{59, "59m"},
		{60, "1h"},
		{61, "1h 1m"},
		{120, "2h"},
		{125, "2h 5m"},
		{600, "10h"},
	}
	for _, tt := range tests {
		got := formatMinutes(tt.min)
		if got != tt.want {
			t.Errorf("formatMinutes(%d) = %q, want %q", tt.min, got, tt.want)
		}
	}
}

func TestPeriodRangeLabel(t *testing.T) {
	tests := []struct{ in, want string }{
		{"daily", "today"},
		{"weekly", "this week"},
		{"monthly", "this month"},
		{"", ""},
		{"unknown", "unknown"}, // passes through verbatim
	}
	for _, tt := range tests {
		if got := periodRangeLabel(tt.in); got != tt.want {
			t.Errorf("periodRangeLabel(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}