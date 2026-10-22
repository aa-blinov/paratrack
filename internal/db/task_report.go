package db

import (
	"context"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// TrackedSecondsByTask returns the seconds each task got inside a period (TASK-04).
// The period is half open, so a day's report ends where the next day starts.
func (d *DB) TrackedSecondsByTask(ctx context.Context, query appmodel.TaskReportQuery) (map[int64]int, error) {
	if query.TeamID <= 0 || !query.End.After(query.Start) {
		return nil, ErrNotFound
	}
	return d.trackedSecondsByTask(ctx, query.TeamID, query.Start, query.End, query.Now)
}
