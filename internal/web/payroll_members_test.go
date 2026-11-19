package web

import "testing"

// A repeat of a period names the run it repeats (UX-44): three same-looking
// calculations are told apart by that one word, and the first run of the
// period stays unmarked. The list is newest first, so the repeat points at the
// older document.
func TestMarkPeriodDuplicates(t *testing.T) {
	items := []payrollSummary{
		{Number: "PAY-3", Period: "24–30 ноября"},
		{Number: "PAY-2", Period: "17–23 ноября"},
		{Number: "PAY-1", Period: "24–30 ноября"},
	}
	markPeriodDuplicates(items)
	if items[0].DuplicateOf != "" {
		t.Fatalf("the newest run of the period carries %q, want no mark", items[0].DuplicateOf)
	}
	if items[1].DuplicateOf != "" {
		t.Fatalf("a sole run carries %q, want no mark", items[1].DuplicateOf)
	}
	if items[2].DuplicateOf != "PAY-3" {
		t.Fatalf("the repeat of PAY-3 carries %q", items[2].DuplicateOf)
	}
}
