package web

import "testing"

func TestColorForReturnsNeutralGrey(t *testing.T) {
	want := "#6b7280"
	for _, name := range []string{"reading", "WORK", "Coding", "deep-work", ""} {
		if got := colorFor(name); got != want {
			t.Errorf("colorFor(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestColorForDeterministic(t *testing.T) {
	a := colorFor("reading")
	b := colorFor("reading")
	if a != b {
		t.Fatalf("colorFor not deterministic: %q vs %q", a, b)
	}
}

func TestColorForCaseInsensitive(t *testing.T) {
	// No-op palette, but the contract still has to hold for any
	// name-based hash we might reintroduce later.
	first := colorFor("Reading")
	for _, n := range []string{"READING", "readING", "reading"} {
		if got := colorFor(n); got != first {
			t.Errorf("colorFor(%q) = %q, want %q", n, got, first)
		}
	}
}