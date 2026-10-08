package model

import (
	"hash/fnv"
	"strings"
)

// Palette — the colours an activity can wear. Same name → same colour across
// the dashboard, stats and graph, so a person learns to recognise their own
// work by its mark. The set is muted so the marks read as identity cues
// rather than decoration, and it holds no pure red: that is reserved for the
// destructive actions (Stop / Delete).
//
// Ten slots is the honest limit. Measured, more colours make marks *harder*
// to tell apart, not easier — twenty evenly spaced hues drop the closest
// pair to ΔE 9.9 for normal vision and to 1.3 under protanopia, which is the
// same colour. So past ten activities the wheel turns over, and the mark
// becomes a hint rather than an identifier.
var Palette = []string{
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

// ColorForName is the legacy mapping: the name hashed into a palette slot.
// It no longer decides what an activity wears — that is stored — but it is
// what the activities already on a server look like, so the backfill keeps a
// mark unchanged wherever it is already unambiguous.
func ColorForName(name string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(name))))
	return Palette[int(h.Sum32())%len(Palette)]
}

// NextColor returns the first palette colour that is not already worn by this
// workspace, so a new activity starts out distinguishable from the existing
// ones. Once every slot is taken the wheel starts again — the limit is
// written down in Palette rather than hidden.
func NextColor(used []string) string {
	taken := make(map[string]bool, len(used))
	for _, color := range used {
		taken[strings.ToLower(strings.TrimSpace(color))] = true
	}
	for _, color := range Palette {
		if !taken[color] {
			return color
		}
	}
	return Palette[0]
}
