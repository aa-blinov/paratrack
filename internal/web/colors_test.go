package web

import (
	"strings"
	"testing"
)

func TestColorForDeterministic(t *testing.T) {
	for _, name := range []string{"reading", "WORK", "deep-work", "morning routine", ""} {
		if got := colorFor(name); got != colorFor(name) {
			t.Errorf("colorFor(%q) not deterministic: %q vs %q", name, got, colorFor(name))
		}
	}
}

func TestColorForCaseInsensitive(t *testing.T) {
	// Same activity in different cases → same colour.
	cases := []string{"Reading", "READING", "readING", "reading"}
	first := colorFor(cases[0])
	for _, n := range cases[1:] {
		if got := colorFor(n); got != first {
			t.Errorf("colorFor(%q) = %q, want %q", n, got, first)
		}
	}
}

func TestColorForTrimsWhitespace(t *testing.T) {
	// Surrounding whitespace must not shift the hash slot.
	if a, b := colorFor("reading"), colorFor("  reading\n"); a != b {
		t.Errorf("whitespace shifts the colour: %q vs %q", a, b)
	}
}

func TestColorForInPalette(t *testing.T) {
	for _, name := range []string{
		"reading", "work", "writing", "exercise", "coding",
		"study", "deep-work", "morning", "evening", "lunch",
		"side-project", "1-on-1", "responding-to-slem",
	} {
		got := colorFor(name)
		ok := false
		for _, p := range palette {
			if got == p {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("colorFor(%q) = %q, not in palette", name, got)
		}
	}
}

func TestColorForDistributes(t *testing.T) {
	// A handful of unrelated names should not collapse onto one slot,
	// otherwise the per-activity coding collapses to one colour.
	seen := map[string]bool{}
	for _, n := range []string{"reading", "work", "writing", "exercise", "coding"} {
		seen[colorFor(n)] = true
	}
	if len(seen) < 3 {
		t.Errorf("expected ≥3 distinct colours across 5 names, got %d", len(seen))
	}
}

func TestColorForNoPureRed(t *testing.T) {
	// The pure destructive-action red is reserved for Stop / Delete —
	// a name must never land on a Stop-button red.
	const destructiveRed = "#e11d48"
	for _, name := range []string{"reading", "work", "writing", "exercise", "coding", "deep-work"} {
		if got := colorFor(name); strings.EqualFold(got, destructiveRed) {
			t.Errorf("colorFor(%q) = %q, must avoid destructive-action red", name, got)
		}
	}
}