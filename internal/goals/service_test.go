package goals

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

type progressReaderStub struct{ progress []model.GoalProgress }

type goalWriterStub struct {
	setCalls   int
	unsetCalls int
	setRequest appmodel.GoalSetRequest
	unsetReq   appmodel.GoalUnsetRequest
}

func (s *goalWriterStub) UpsertGoalForManager(context.Context, appmodel.GoalUpsertRequest) (model.Goal, error) {
	panic("single goal API should not be called")
}
func (s *goalWriterStub) DeleteGoalForManager(context.Context, appmodel.GoalDeleteRequest) error {
	panic("single goal API should not be called")
}
func (s *goalWriterStub) UpsertGoalsForManager(_ context.Context, request appmodel.GoalSetRequest) ([]model.Goal, error) {
	s.setCalls++
	s.setRequest = request
	return nil, nil
}
func (s *goalWriterStub) DeleteGoalsForManager(_ context.Context, request appmodel.GoalUnsetRequest) (int, error) {
	s.unsetCalls++
	s.unsetReq = request
	return 2, nil
}

type managementActivityReader struct {
	activities []model.Activity
	teamID     int64
	archived   bool
}

func (s *managementActivityReader) ListActivities(_ context.Context, teamID int64, archived bool) ([]model.Activity, error) {
	s.teamID, s.archived = teamID, archived
	return s.activities, nil
}

type managementProgressReader struct {
	progress []model.GoalProgress
	teamID   int64
	now      time.Time
}

func (s *managementProgressReader) ListGoals(context.Context, int64, *int64) ([]model.Goal, error) {
	return nil, nil
}

func (s *managementProgressReader) ProgressForGoals(_ context.Context, teamID int64, now time.Time) ([]model.GoalProgress, error) {
	s.teamID, s.now = teamID, now
	return s.progress, nil
}

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

func TestSetForManagerValidatesWholeBatchBeforeWriting(t *testing.T) {
	writer := &goalWriterStub{}
	service := &Service{deps: Dependencies{Writes: writer}}
	request := appmodel.GoalSetRequest{TeamID: 3, CallerID: 7, ActivityName: "  Focus ", Targets: []appmodel.GoalTarget{{Period: "daily", Minutes: 30}, {Period: "bogus", Minutes: 20}}}
	if _, err := service.SetForManager(context.Background(), request); !errors.Is(err, ErrInvalidPeriod) {
		t.Fatalf("SetForManager error = %v, want invalid period", err)
	}
	if writer.setCalls != 0 {
		t.Fatalf("writer called %d times for invalid batch", writer.setCalls)
	}
	request.Targets[1].Period = "weekly"
	if _, err := service.SetForManager(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if writer.setCalls != 1 || writer.setRequest.ActivityName != "Focus" || len(writer.setRequest.Targets) != 2 {
		t.Fatalf("batch write = calls %d, request %+v", writer.setCalls, writer.setRequest)
	}
}

func TestUnsetForManagerUsesSingleBatchWrite(t *testing.T) {
	writer := &goalWriterStub{}
	service := &Service{deps: Dependencies{Writes: writer}}
	request := appmodel.GoalUnsetRequest{TeamID: 3, CallerID: 7, ActivityName: " Focus ", Periods: []string{"daily", "weekly"}}
	if count, err := service.UnsetForManager(context.Background(), request); err != nil || count != 2 {
		t.Fatalf("UnsetForManager = %d, %v", count, err)
	}
	if writer.unsetCalls != 1 || writer.unsetReq.ActivityName != "Focus" || len(writer.unsetReq.Periods) != 2 {
		t.Fatalf("batch unset = calls %d, request %+v", writer.unsetCalls, writer.unsetReq)
	}
	request.Periods[1] = "yearly"
	if _, err := service.UnsetForManager(context.Background(), request); !errors.Is(err, ErrInvalidPeriod) {
		t.Fatalf("invalid UnsetForManager error = %v", err)
	}
	if writer.unsetCalls != 1 {
		t.Fatalf("writer called after invalid unset batch: %d", writer.unsetCalls)
	}
}

func TestManagementAssemblesScopedActivityAndProgressSnapshot(t *testing.T) {
	now := time.Date(2026, time.October, 4, 12, 0, 0, 0, time.UTC)
	activity := model.Activity{ID: 7, TeamID: 3, Name: "Focus"}
	goal := model.GoalProgress{ActivityName: "Focus", Goal: model.Goal{ActivityID: 7, Period: "daily"}}
	activities := &managementActivityReader{activities: []model.Activity{activity}}
	progress := &managementProgressReader{progress: []model.GoalProgress{goal}}
	service := &Service{deps: Dependencies{Activities: activities, Goals: progress}}

	snapshot, err := service.Management(context.Background(), appmodel.GoalManagementQuery{TeamID: 3, Now: now})
	if err != nil {
		t.Fatalf("Management: %v", err)
	}
	if len(snapshot.Activities) != 1 || snapshot.Activities[0] != activity || len(snapshot.Progress) != 1 || snapshot.Progress[0] != goal {
		t.Fatalf("management snapshot = %+v", snapshot)
	}
	if activities.teamID != 3 || activities.archived || progress.teamID != 3 || !progress.now.Equal(now) {
		t.Fatalf("read scope = activity(team=%d archived=%v), progress(team=%d now=%s)", activities.teamID, activities.archived, progress.teamID, progress.now)
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
