package projects

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

func TestListRejectsUnscopedWorkspace(t *testing.T) {
	service := &Service{}
	if _, err := service.List(context.Background(), 0, false); !errors.Is(err, ErrInvalidTeam) {
		t.Fatalf("List with no workspace error = %v, want %v", err, ErrInvalidTeam)
	}
}

func TestSumRecentProjectTimeUsesRequestedWindowAndExcludesPause(t *testing.T) {
	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	through := from.Add(24 * time.Hour)
	pausedAt := from.Add(time.Hour)
	recent := []model.ActiveSession{{Session: model.Session{
		StartAt: from, Paused: true, PausedAt: &pausedAt,
		AccumulatedSeconds: 3600,
	}}}

	got, err := sumRecentProjectTime(recent, from, through)
	if err != nil {
		t.Fatalf("sumRecentProjectTime() error = %v", err)
	}
	if got != 3600 {
		t.Fatalf("sumRecentProjectTime() = %d, want 3600", got)
	}
}
