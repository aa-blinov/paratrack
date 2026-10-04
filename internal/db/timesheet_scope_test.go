package db

import (
	"testing"
	"time"
)

func TestTimesheetRejectsMissingWorkspaceScope(t *testing.T) {
	var d *DB
	weekStart := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if _, err := d.ListTimesheet(t.Context(), 0, weekStart, weekStart.Add(time.Hour)); err != ErrNotFound {
		t.Fatalf("ListTimesheet error = %v, want not found", err)
	}
}
