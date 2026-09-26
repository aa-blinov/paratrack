package web

import (
	"hash/fnv"
	"math"
	"strings"
)

// palette — the deterministic backing store for per-activity colours.
// Same name → same colour across the dashboard, stats and graph; the
// muted set below avoids the pure red reserved for destructive
// actions (Stop / Delete).
//
// Colours are desaturated Tailwind-style hexes so the activity-marks
// read as identity cues, not decoration.  Each new activity name
// hashes to one of these slots; collisions fall back to a tonal
// mix-blend so the same name still produces the same hue.
var palette = []string{
	"#6366f1", // indigo
	"#0ea5e9", // sky
	"#14b8a6", // teal
	"#10b981", // emerald
	"#84cc16", // lime
	"#f59e0b", // amber
	"#f97316", // orange
	"#8b5cf6", // violet
	"#a855f7", // purple
	"#ec4899", // pink
}

// colorFor hashes the lower-cased activity name and maps it onto the
// palette so the same activity always renders the same colour, and
// different activities land on distinguishable slots.
func colorFor(name string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(name))))
	return palette[int(h.Sum32())%len(palette)]
}
// inkFor returns the foreground (#000 / #fff) with the higher WCAG
// contrast against the given hex surface. Used for badges and chips
// that take a user-authored project colour as their background — a
// hard-coded #fff fails as soon as the project colour is pale.
func inkFor(bg string) string {
	c := strings.TrimSpace(bg)
	if strings.HasPrefix(c, "#") {
		c = c[1:]
	}
	if len(c) == 3 {
		c = string([]byte{c[0], c[0], c[1], c[1], c[2], c[2]})
	}
	if len(c) != 6 {
		return "#ffffff"
	}
	var r, g, b int
	if _, err := parseHexPair(c[0:2], &r); err != nil {
		return "#ffffff"
	}
	parseHexPair(c[2:4], &g)
	parseHexPair(c[4:6], &b)
	// sRGB relative luminance
	f := func(v int) float64 {
		x := float64(v) / 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	l := 0.2126*f(r) + 0.7152*f(g) + 0.0722*f(b)
	// contrast against white vs black
	white := (1.0 + 0.05) / (l + 0.05)
	black := (l + 0.05) / 0.05
	if black >= white {
		return "#111111"
	}
	return "#ffffff"
}

func parseHexPair(s string, out *int) (int, error) {
	v := 0
	for _, r := range s {
		v <<= 4
		switch {
		case r >= '0' && r <= '9':
			v += int(r - '0')
		case r >= 'a' && r <= 'f':
			v += int(r-'a') + 10
		case r >= 'A' && r <= 'F':
			v += int(r-'A') + 10
		default:
			*out = 0
			return 0, nil
		}
	}
	*out = v
	return v, nil
}
