package web

import (
	"hash/fnv"
	"strings"
)

// palette — small set of muted colors used as the per-activity marker.
// Same hash → same color, so an activity keeps its identity across the
// dashboard, stats and graph. Kept deliberately short so the UI reads
// as monochrome with sparing accents rather than a rainbow.
var palette = []string{
	"#6366f1", // primary
	"#10b981", // success
	"#f59e0b", // warning
	"#06b6d4", // info
}

func colorFor(name string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(name)))
	return palette[int(h.Sum32())%len(palette)]
}
