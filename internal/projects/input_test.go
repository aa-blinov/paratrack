package projects

import (
	"errors"
	"testing"
)

func TestNormalizeProjectInput(t *testing.T) {
	name, slug, color, err := normalizeProjectInput("  Ромашка Studio  ", "", "")
	if err != nil {
		t.Fatalf("normalize valid project: %v", err)
	}
	if name != "Ромашка Studio" || slug != "romashka-studio" || color != "#7c8499" {
		t.Fatalf("normalized = (%q, %q, %q)", name, slug, color)
	}

	for _, test := range []struct {
		caseName string
		name     string
		slug     string
		color    string
		want     error
	}{
		{caseName: "empty name", want: ErrInvalidName},
		{caseName: "invalid slug", name: "Name", slug: "bad/slug", color: "#123456", want: ErrInvalidSlug},
		{caseName: "invalid color", name: "Name", slug: "name", color: "red", want: ErrInvalidColor},
	} {
		t.Run(test.caseName, func(t *testing.T) {
			_, _, _, err := normalizeProjectInput(test.name, test.slug, test.color)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}
