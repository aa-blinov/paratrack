package invoicing

import (
	"context"
	"fmt"
	"time"
)

// UnapprovedWeeks names the weeks of a period (start inclusive, end exclusive)
// that still carry billable time outside every approved week (FIN-12). The
// store cuts them in the workspace's own zone and from its own first weekday,
// the same week the manager can approve, so the answer agrees with the
// timesheet. A week is named by its first day.
func (s *Service) UnapprovedWeeks(ctx context.Context, teamID, projectID int64, start, end time.Time) ([]time.Time, error) {
	if teamID <= 0 {
		return nil, ErrInvalidTeam
	}
	names, err := s.reader.ListUnapprovedTimeWeeks(ctx, teamID, projectID, start, end)
	if err != nil {
		return nil, fmt.Errorf("list unapproved weeks: %w", err)
	}
	var weeks []time.Time
	for _, name := range names {
		at, err := time.Parse("2006-01-02", name)
		if err != nil {
			return nil, fmt.Errorf("unapproved week %q: %w", name, err)
		}
		weeks = append(weeks, at)
	}
	return weeks, nil
}
