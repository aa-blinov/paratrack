package model

import (
	"errors"
	"testing"
	"time"
)

func TestResolveSessionIntervalEdit(t *testing.T) {
	start := time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	selectedStart := start.Add(2 * time.Hour)
	duration := 45 * 60

	tests := []struct {
		name        string
		edit        SessionIntervalEdit
		wantStart   *time.Time
		wantEnd     *time.Time
		wantSeconds *int
		wantErr     error
	}{
		{
			name:        "duration uses selected start",
			edit:        SessionIntervalEdit{StartAt: &selectedStart, DurationSeconds: &duration},
			wantStart:   &selectedStart,
			wantEnd:     timePointer(selectedStart.Add(time.Duration(duration) * time.Second)),
			wantSeconds: intPointer(duration),
		},
		{
			name:        "recompute uses selected interval",
			edit:        SessionIntervalEdit{StartAt: &selectedStart, EndAt: timePointer(selectedStart.Add(2 * time.Hour)), RecomputeDuration: true},
			wantStart:   &selectedStart,
			wantEnd:     timePointer(selectedStart.Add(2 * time.Hour)),
			wantSeconds: intPointer(2 * 60 * 60),
		},
		{
			name:    "invalid interval rejected",
			edit:    SessionIntervalEdit{StartAt: timePointer(end.Add(time.Minute))},
			wantErr: ErrSessionPeriodInvalid,
		},
		{
			name:    "negative duration rejected",
			edit:    SessionIntervalEdit{DurationSeconds: intPointer(-1)},
			wantErr: ErrSessionLengthInvalid,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ResolveSessionIntervalEdit(start, &end, test.edit)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("ResolveSessionIntervalEdit() error = %v, want %v", err, test.wantErr)
			}
			if err != nil {
				return
			}
			if !sameTimePointer(got.StartAt, test.wantStart) || !sameTimePointer(got.EndAt, test.wantEnd) || !sameIntPointer(got.AccumulatedSeconds, test.wantSeconds) {
				t.Fatalf("ResolveSessionIntervalEdit() = %+v", got)
			}
		})
	}
}

func sameTimePointer(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Equal(*right)
}

func sameIntPointer(left, right *int) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func timePointer(value time.Time) *time.Time { return &value }

func intPointer(value int) *int { return &value }
