package db

import (
	"testing"
)

func TestGetOrCreateActivity_CaseInsensitive(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()

	a, err := d.GetOrCreateActivity(ctx, 0, "work")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "work" {
		t.Errorf("expected normalised name 'work', got %q", a.Name)
	}

	// Different case → same id, canonical lowercase name.
	for _, variant := range []string{"WORK", "Work", "wOrK"} {
		got, err := d.GetOrCreateActivity(ctx, 0, variant)
		if err != nil {
			t.Errorf("GetOrCreateActivity(%q): %v", variant, err)
			continue
		}
		if got.ID != a.ID || got.Name != "work" {
			t.Errorf("variant %q: got id=%d name=%q, want id=%d name='work'",
				variant, got.ID, got.Name, a.ID)
		}
	}
}

func TestGetActivityByName_CaseInsensitive(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	a, err := d.GetOrCreateActivity(ctx, 0, "Reading")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "Reading" {
		t.Errorf("the name keeps the case it was typed in, got %q", a.Name)
	}
	b, err := d.GetActivityByName(ctx, 0, "READING")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID {
		t.Errorf("case-insensitive lookup should find same id, got %d vs %d", a.ID, b.ID)
	}
}

func TestCreateActivity_TrimsAndKeepsCase(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	a, err := d.CreateActivity(ctx, 0, "  Writing  ")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "Writing" {
		t.Errorf("expected trimmed 'Writing', got %q", a.Name)
	}
	// Cyrillic folds too: "Вёрстка" and "вёрстка" are one activity.
	x, _ := d.GetOrCreateActivity(ctx, 0, "Вёрстка")
	y, _ := d.GetOrCreateActivity(ctx, 0, "вёрстка")
	if x.ID != y.ID || y.Name != "Вёрстка" {
		t.Errorf("Cyrillic case: %d/%q vs %d/%q", x.ID, x.Name, y.ID, y.Name)
	}
}
