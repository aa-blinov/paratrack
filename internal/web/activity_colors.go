package web

import (
	"net/http"
	"strings"

	"github.com/aa-blinov/paratrack/internal/model"
)

// activityColors returns the workspace's marks keyed by activity name, lower
// cased. Views read it once and pass it down rather than re-reading the
// catalogue per row.
func (s *Server) activityColors(r *http.Request) map[string]string {
	ctx := r.Context()
	colors := map[string]string{}
	list, err := s.services.Tracking.Queries.ListActivities(ctx, teamID(r), true)
	if err == nil {
		for _, activity := range list {
			colors[strings.ToLower(activity.Name)] = activity.Color
		}
	}
	return colors
}

// activityColor resolves one activity's mark by name. An activity with no
// stored colour — a row the backfill has not reached — falls back to the
// legacy hash so it still renders in the colour it had before.
func activityColor(colors map[string]string, name string) string {
	if color, ok := colors[strings.ToLower(strings.TrimSpace(name))]; ok && color != "" {
		return color
	}
	return model.ColorForName(name)
}

// activityMark is the stored colour, falling back to the legacy hash when the
// row has none — a workspace the backfill has not reached still renders in the
// colour it had before, rather than in nothing.
func activityMark(stored, name string) string {
	if strings.TrimSpace(stored) != "" {
		return stored
	}
	return model.ColorForName(name)
}

// activityColorsFromActivities keys marks by name from activities already in
// hand — the dashboard snapshot carries them, so it needs no extra read.
func activityColorsFromActivities(list []model.Activity) map[string]string {
	colors := make(map[string]string, len(list))
	for _, activity := range list {
		colors[strings.ToLower(activity.Name)] = activity.Color
	}
	return colors
}
