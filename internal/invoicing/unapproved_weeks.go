package invoicing

import (
	"context"
	"fmt"
	"time"
)

// UnapprovedWeeks names the weeks of a period (start inclusive, end exclusive)
// that still carry billable time outside every approved week (FIN-12). A week is
// named by its Monday in UTC, the day the timesheet starts its weeks on. The
// store returns the days in order, so the weeks come out in order too.
func (s *Service) UnapprovedWeeks(ctx context.Context, teamID, projectID int64, start, end time.Time) ([]time.Time, error) {
	if teamID <= 0 {
		return nil, ErrInvalidTeam
	}
	days, err := s.reader.ListUnapprovedTimeDays(ctx, teamID, projectID, start, end)
	if err != nil {
		return nil, fmt.Errorf("list unapproved days: %w", err)
	}
	var weeks []time.Time
	seen := make(map[string]bool)
	for _, day := range days {
		at, err := time.Parse("2006-01-02", day)
		if err != nil {
			return nil, fmt.Errorf("unapproved day %q: %w", day, err)
		}
		monday := at.AddDate(0, 0, -((int(at.Weekday()) + 6) % 7))
		if key := monday.Format("2006-01-02"); !seen[key] {
			seen[key] = true
			weeks = append(weeks, monday)
		}
	}
	return weeks, nil
}
