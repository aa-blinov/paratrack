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
