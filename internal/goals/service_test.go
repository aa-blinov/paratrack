package goals

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

type progressReaderStub struct{ progress []model.GoalProgress }

func (s progressReaderStub) ListGoals(context.Context, int64, *int64) ([]model.Goal, error) {
	return nil, nil
}

func (s progressReaderStub) ProgressForGoals(context.Context, int64, time.Time) ([]model.GoalProgress, error) {
	return s.progress, nil
}

func TestProgressRequiresWorkspace(t *testing.T) {
	service := &Service{}
	if _, err := service.Progress(context.Background(), 0, time.Now()); !errors.Is(err, ErrInvalidTeam) {
		t.Fatalf("Progress with no workspace error = %v, want %v", err, ErrInvalidTeam)
	}
}

func TestNewlyAchievedAfterSessionKeepsThresholdPolicyInWorkflow(t *testing.T) {
	service := &Service{deps: Dependencies{Goals: progressReaderStub{progress: []model.GoalProgress{
		{Goal: model.Goal{ActivityID: 3, TargetMinutes: 60, Period: "daily"}, ActivityName: "Focus", AchievedMinutes: 70, PercentComplete: 116},
		{Goal: model.Goal{ActivityID: 3, TargetMinutes: 40, Period: "weekly"}, ActivityName: "Focus", AchievedMinutes: 70, PercentComplete: 175},
		{Goal: model.Goal{ActivityID: 4, TargetMinutes: 60}, ActivityName: "Other", AchievedMinutes: 100, PercentComplete: 166},
	}}}}
	got, err := service.NewlyAchievedAfterSession(context.Background(), 7, 3, 20*60, time.Date(2026, time.May, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Goal.Period != "daily" {
		t.Fatalf("newly achieved goal = %+v, want daily threshold crossing", got)
	}
}

func TestNewlyAchievedAfterSessionsAggregatesByActivity(t *testing.T) {
	service := &Service{deps: Dependencies{Goals: progressReaderStub{progress: []model.GoalProgress{
		{Goal: model.Goal{ActivityID: 3, TargetMinutes: 60, Period: "daily"}, ActivityName: "Focus", AchievedMinutes: 70, PercentComplete: 116},
		{Goal: model.Goal{ActivityID: 3, TargetMinutes: 55, Period: "weekly"}, ActivityName: "Focus", AchievedMinutes: 70, PercentComplete: 127},
		{Goal: model.Goal{ActivityID: 4, TargetMinutes: 60}, ActivityName: "Other", AchievedMinutes: 100, PercentComplete: 166},
	}}}}
	got, err := service.NewlyAchievedAfterSessions(context.Background(), 7, map[int64]int{3: 20 * 60}, time.Date(2026, time.May, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Goal.Period != "daily" || got[1].Goal.Period != "weekly" {
		t.Fatalf("newly achieved goals = %+v, want daily and weekly goals", got)
	}
}
