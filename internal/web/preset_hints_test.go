package web

import (
	"strings"
	"testing"
)

// The solo preset says what it leaves closed and that a team needs another preset (ADM-16).
func TestSoloPresetSaysWhatItLeavesClosed(t *testing.T) {
	e := newAPIEnv(t)
	e.register("preset-hints@x.test")
	body := readBody(t, e.do("GET", "/settings/sections", nil, nil))
	// The page reads the length of every preset's list: a null list breaks the whole screen.
	if strings.Contains(body, `"Off":null`) {
		t.Fatal("a preset's closed list serialises as null, the sections page cannot render")
	}
	page := reactData[sectionsPage](t, body)
	for _, preset := range page.Presets {
		switch preset.Key {
		case "solo":
			if len(preset.Off) == 0 || preset.Hint == "" {
				t.Fatalf("the solo preset does not say what is closed or where a team goes: %+v", preset)
			}
		case "studio":
			if len(preset.Off) != 0 || preset.Hint != "" {
				t.Fatalf("the studio preset opened no section yet says %+v", preset)
			}
		}
	}
}
