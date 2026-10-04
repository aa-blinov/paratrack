package db

import (
	"errors"
	"testing"

	"github.com/aa-blinov/paratrack/internal/money"
)

func TestSummarizeTimesheetBucketsRejectsOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	buckets := map[int64]*timesheetAccumulator{
		1: {name: "A", total: maxInt, secs: [7]int{maxInt}},
		2: {name: "B", total: 1, secs: [7]int{1}},
	}
	if _, err := summarizeTimesheetBuckets(buckets); !errors.Is(err, money.ErrOverflow) {
		t.Fatalf("summarizeTimesheetBuckets() error = %v, want money.ErrOverflow", err)
	}
}
