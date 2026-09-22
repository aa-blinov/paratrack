package web

import "hash/fnv"

// palette is a curated 12-color set that stays distinguishable in both
// light and dark themes. Used as the deterministic backing store for
// per-activity colours.
//
// Note: pure red (#ef4444) is intentionally omitted — it would clash with
// the destructive-action red used for Stop / Delete buttons.
var palette = []string{
	"#6366f1", // indigo
	"#10b981", // emerald
	"#f59e0b", // amber
	"#06b6d4", // cyan
	"#8b5cf6", // violet
	"#ec4899", // pink
	"#84cc16", // lime
	"#f97316", // orange
	"#14b8a6", // teal
	"#a855f7", // purple
	"#0ea5e9", // sky
	"#e11d48", // rose (still distinct from the danger button red)
}

// colorFor returns a palette entry derived from the activity name's
// hash, so the same name always renders the same colour across the
// dashboard, stats and graph pages.
func colorFor(name string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return palette[int(h.Sum32())%len(palette)]
}
