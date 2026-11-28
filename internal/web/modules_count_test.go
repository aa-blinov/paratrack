package web

import "testing"

// A member counts only the sections they can open: a manager-only section that
// the team switched on is not open to them, and the mode line must not say it is.
func TestSectionsOpenForMemberLeavesOutManagerOnly(t *testing.T) {
	all := map[string]bool{}
	managerOnly := 0
	for _, m := range modules {
		all[m.Key] = true
		if m.Manage {
			managerOnly++
		}
	}
	if managerOnly == 0 {
		t.Fatal("the catalogue should have manager-only sections for this check")
	}
	open, total := sectionsOpenFor(all, false)
	if open != total-managerOnly || total != len(modules) {
		t.Fatalf("member with everything on: got %d of %d, want %d of %d", open, total, total-managerOnly, total)
	}
	open, _ = sectionsOpenFor(all, true)
	if open != len(modules) {
		t.Fatalf("manager with everything on: got %d of %d", open, len(modules))
	}
}
