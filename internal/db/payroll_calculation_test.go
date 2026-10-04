package db

import (
	"testing"
	"time"
)

func TestBuildPayrollLinesRejectsMissingWorkspaceScope(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	_, err := buildPayrollLines(t.Context(), nil, 0, start, start.AddDate(0, 0, 1), false, start)
	if err != ErrInvalidPayrollQuery {
		t.Fatalf("buildPayrollLines error = %v, want invalid payroll query", err)
	}
}
