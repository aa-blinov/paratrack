package web

import "testing"

func TestIconHTMLOnlyEmitsSafeMarkup(t *testing.T) {
	if got, want := string(iconHTML("play", "icon icon-sm")), `<svg class="icon icon-sm" aria-hidden="true"><use href="/static/icons.svg#i-play"></use></svg>`; got != want {
		t.Fatalf("iconHTML() = %q, want %q", got, want)
	}
	for _, input := range []struct {
		name  string
		class string
	}{
		{name: `play" onload="alert(1)`},
		{name: "play", class: `icon" onclick="alert(1)`},
	} {
		if got := iconHTML(input.name, input.class); got != "" {
			t.Errorf("iconHTML(%q, %q) = %q, want empty markup", input.name, input.class, got)
		}
	}
	if got := iconHTML("play", "icon", "extra"); got != "" {
		t.Errorf("iconHTML() with multiple classes = %q, want empty markup", got)
	}
}
