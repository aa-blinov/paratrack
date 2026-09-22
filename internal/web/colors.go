package web

import (
	"hash/fnv"
	"strings"
)

// palette — the full Tailwind / DaisyUI colour set. Used as the
// deterministic backing store for per-activity colours so the same
// name — regardless of how the user typed it — always renders the same
// colour across the dashboard, stats and graph.
//
// Pure red (#ef4444) is intentionally omitted — it would clash with the
// destructive-action red used for Stop / Delete buttons.
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
	"#e11d48", // rose
}

func colorFor(name string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(name)))
	return palette[int(h.Sum32())%len(palette)]
}
