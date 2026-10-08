package web

import (
	"math"
	"strings"
)

// projectPalette — the colours a project gets when nobody picks one. The old
// default was a single constant, so every project created through the form
// came out the same purple and the dot beside its name identified nothing.
var projectPalette = []string{
	"#6366f1", "#14b8a6", "#f59e0b", "#8b5cf6", "#10b981",
	"#ec4899", "#0ea5e9", "#84cc16", "#f97316", "#a855f7",
}

// nextProjectColor returns the first palette colour this workspace is not
// already wearing, so a new project starts out distinguishable. Past the
// palette the wheel turns over — the same honest limit as the activity marks.
func nextProjectColor(used []string) string {
	taken := make(map[string]bool, len(used))
	for _, color := range used {
		taken[strings.ToLower(strings.TrimSpace(color))] = true
	}
	for _, color := range projectPalette {
		if !taken[color] {
			return color
		}
	}
	return projectPalette[0]
}

// Activity marks live in internal/model now: they are stored per activity,
// not derived from the name, and the backfill that assigned them runs in the
// database layer. The hash that used to decide a colour is still there as
// model.ColorForName, because it is what existing rows already look like.

// inkFor returns the foreground (#000 / #fff) with the higher WCAG
// contrast against the given hex surface. Used for badges and chips
// that take a user-authored project colour as their background — a
// hard-coded #fff fails as soon as the project colour is pale.
func inkFor(bg string) string {
	c := strings.TrimSpace(bg)
	c = strings.TrimPrefix(c, "#")
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
