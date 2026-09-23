package web

import (
	"hash/fnv"
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