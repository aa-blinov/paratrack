package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

func TestTimesheetRejectsMissingWorkspaceScope(t *testing.T) {
	var d *DB
	weekStart := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if _, err := d.ListTimesheet(t.Context(), appmodel.TimesheetRequest{TeamID: 0, WeekStart: weekStart, Now: weekStart.Add(time.Hour)}); err != ErrNotFound {
		t.Fatalf("ListTimesheet error = %v, want not found", err)
	}
}
