package web

import "testing"

// The dashboard shows the mode line as "5 of 9 sections open" so the mode
// explains itself where its effect is felt. That count has to come from the
// same catalogue the sections screen edits, not from a client-side guess.
func TestSectionsCounts(t *testing.T) {
	solo := map[string]bool{"graph": true, "goals": true, "tags": true, "integrations": true, "import": true}
	open, total := sectionsCounts(solo)
	if total != len(modules) {
		t.Fatalf("total should be the whole catalogue: got %d, want %d", total, len(modules))
	}
	if open != 5 {
		t.Fatalf("solo should open five sections: got %d", open)
	}

	all := map[string]bool{}
	for _, m := range modules {
		all[m.Key] = true
	}
	open, total = sectionsCounts(all)
	if open != total {
		t.Fatalf("everything on should read as all open: %d of %d", open, total)
	}

	open, total = sectionsCounts(map[string]bool{})
	if open != 0 || total != len(modules) {
		t.Fatalf("nothing on: got %d of %d", open, total)
	}
}

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
