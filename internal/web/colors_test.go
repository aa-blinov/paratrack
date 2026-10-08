package web

import (
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/model"
)

func TestActivityColorForNameDeterministic(t *testing.T) {
	for _, name := range []string{"reading", "WORK", "deep-work", "morning routine", ""} {
		if got := model.ColorForName(name); got != model.ColorForName(name) {
			t.Errorf("model.ColorForName(%q) not deterministic: %q vs %q", name, got, model.ColorForName(name))
		}
	}
}

func TestActivityColorForNameCaseInsensitive(t *testing.T) {
	// Same activity in different cases → same colour.
	cases := []string{"Reading", "READING", "readING", "reading"}
	first := model.ColorForName(cases[0])
	for _, n := range cases[1:] {
		if got := model.ColorForName(n); got != first {
			t.Errorf("model.ColorForName(%q) = %q, want %q", n, got, first)
		}
	}
}

func TestActivityColorForNameTrimsWhitespace(t *testing.T) {
	// Surrounding whitespace must not shift the hash slot.
	if a, b := model.ColorForName("reading"), model.ColorForName("  reading\n"); a != b {
		t.Errorf("whitespace shifts the colour: %q vs %q", a, b)
	}
}

func TestActivityColorForNameInPalette(t *testing.T) {
	for _, name := range []string{
		"reading", "work", "writing", "exercise", "coding",
		"study", "deep-work", "morning", "evening", "lunch",
		"side-project", "1-on-1", "responding-to-slem",
	} {
		got := model.ColorForName(name)
		ok := false
		for _, p := range model.Palette {
			if got == p {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("model.ColorForName(%q) = %q, not in model.Palette", name, got)
		}
	}
}

func TestActivityColorForNameDistributes(t *testing.T) {
	// A handful of unrelated names should not collapse onto one slot,
	// otherwise the per-activity coding collapses to one colour.
	seen := map[string]bool{}
	for _, n := range []string{"reading", "work", "writing", "exercise", "coding"} {
		seen[model.ColorForName(n)] = true
	}
	if len(seen) < 3 {
		t.Errorf("expected ≥3 distinct colours across 5 names, got %d", len(seen))
	}
}

func TestActivityColorForNameNoPureRed(t *testing.T) {
	// The pure destructive-action red is reserved for Stop / Delete —
	// a name must never land on a Stop-button red.
	const destructiveRed = "#e11d48"
	for _, name := range []string{"reading", "work", "writing", "exercise", "coding", "deep-work"} {
		if got := model.ColorForName(name); strings.EqualFold(got, destructiveRed) {
			t.Errorf("model.ColorForName(%q) = %q, must avoid destructive-action red", name, got)
		}
	}
}

func TestNextProjectColorSkipsWhatIsTaken(t *testing.T) {
	// The old default was one constant, so every project created through the
	// form came out the same purple. Each new project must get a colour the
	// team is not already wearing.
	var used []string
	seen := map[string]bool{}
	for i := 0; i < len(projectPalette); i++ {
		got := nextProjectColor(used)
		if seen[got] {
			t.Fatalf("project %d got %q again while %v are still free", i, got, used)
		}
		seen[got] = true
		used = append(used, got)
	}
	if len(seen) != len(projectPalette) {
		t.Errorf("expected the whole model.Palette once, got %d distinct of %d", len(seen), len(projectPalette))
	}
}

func TestNextProjectColorIgnoresCaseAndSpace(t *testing.T) {
	first := projectPalette[0]
	if got := nextProjectColor([]string{"  " + strings.ToUpper(first) + "  "}); got == first {
		t.Errorf("nextProjectColor(%q) returned the colour that is already in use", first)
	}
}

func TestNextProjectColorWraps(t *testing.T) {
	// Past the model.Palette the wheel turns over. That is the honest limit of a
	// fixed model.Palette — it must not panic or return nothing.
	all := append([]string(nil), projectPalette...)
	if got := nextProjectColor(all); got != projectPalette[0] {
		t.Errorf("with every colour taken, want %q, got %q", projectPalette[0], got)
	}
	if got := nextProjectColor(nil); got != projectPalette[0] {
		t.Errorf("with nothing taken, want %q, got %q", projectPalette[0], got)
	}
}
