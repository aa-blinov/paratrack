// Package goals owns goal validation and workflows shared by application
// adapters.
package goals

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
)

// ActivityReader provides the activity lookups needed by goal workflows.
type ActivityReader interface {
	ListActivities(context.Context, int64, bool) ([]model.Activity, error)
}

// GoalReader provides configured goals and their progress.
type GoalReader interface {
	ListGoals(context.Context, int64, *int64) ([]model.Goal, error)
	ProgressForGoals(context.Context, int64, time.Time) ([]model.GoalProgress, error)
}

// GoalWriter provides atomic goal mutations, including manager-authorized
// operations that recheck the caller's role in the persistence transaction.
type GoalWriter interface {
	UpsertGoalForManager(context.Context, appmodel.GoalUpsertRequest) (model.Goal, error)
	DeleteGoalForManager(context.Context, appmodel.GoalDeleteRequest) error
}

type Dependencies struct {
	Activities ActivityReader
	Goals      GoalReader
	Writes     GoalWriter
}

var ErrIncompleteDependencies = errors.New("goal service dependencies are incomplete")

type Service struct{ deps Dependencies }

func New(deps Dependencies) (*Service, error) {
	if depcheck.IsNil(deps.Activities) || depcheck.IsNil(deps.Goals) || depcheck.IsNil(deps.Writes) {
		return nil, ErrIncompleteDependencies
	}
	return &Service{deps: deps}, nil
}

var (
	ErrInvalidTeam          = appmodel.ErrInvalidGoalTeam
	ErrInvalidProgressQuery = errors.New("invalid goal progress query")
	ErrInvalidActivity      = appmodel.ErrInvalidGoalActivity
	ErrInvalidPeriod        = appmodel.ErrInvalidGoalPeriod
	ErrInvalidTarget        = appmodel.ErrInvalidGoalTarget
)

func (s *Service) Activities(ctx context.Context, teamID int64) ([]model.Activity, error) {
	if teamID <= 0 {
		return nil, ErrInvalidTeam
	}
	activities, err := s.deps.Activities.ListActivities(ctx, teamID, false)
	if err != nil {
		return nil, fmt.Errorf("list goal activities: %w", err)
	}
	return activities, nil
}

// Management combines the activity catalog with current goal progress for
// the team management page.
func (s *Service) Management(ctx context.Context, query appmodel.GoalManagementQuery) (appmodel.GoalManagementSnapshot, error) {
	if query.TeamID <= 0 {
		return appmodel.GoalManagementSnapshot{}, ErrInvalidTeam
	}
	if query.Now.IsZero() {
		return appmodel.GoalManagementSnapshot{}, ErrInvalidProgressQuery
	}
	activities, err := s.Activities(ctx, query.TeamID)
	if err != nil {
		return appmodel.GoalManagementSnapshot{}, err
	}
	progress, err := s.Progress(ctx, query.TeamID, query.Now)
	if err != nil {
		return appmodel.GoalManagementSnapshot{}, fmt.Errorf("load goal management progress: %w", err)
	}
	return appmodel.GoalManagementSnapshot{Activities: activities, Progress: progress}, nil
}

func (s *Service) List(ctx context.Context, teamID int64) ([]model.Goal, error) {
	if teamID <= 0 {
		return nil, ErrInvalidTeam
	}
	return s.deps.Goals.ListGoals(ctx, teamID, nil)
}

func (s *Service) Progress(ctx context.Context, teamID int64, now time.Time) ([]model.GoalProgress, error) {
	if teamID <= 0 {
		return nil, ErrInvalidTeam
	}
	return s.deps.Goals.ProgressForGoals(ctx, teamID, now)
}

// NewlyAchievedAfterSession returns the first goal for this activity that
// crossed its threshold after the supplied session ended. It compares the
// current progress with the same progress minus this session's whole minutes.
func (s *Service) NewlyAchievedAfterSession(ctx context.Context, teamID, activityID int64, stoppedSeconds int, now time.Time) (*model.GoalProgress, error) {
	if teamID <= 0 {
		return nil, ErrInvalidTeam
	}
	if activityID <= 0 || stoppedSeconds < 0 {
		return nil, ErrInvalidProgressQuery
	}
	goals, err := s.NewlyAchievedAfterSessions(ctx, teamID, map[int64]int{activityID: stoppedSeconds}, now)
	if err != nil || len(goals) == 0 {
		return nil, err
	}
	return &goals[0], nil
}

// NewlyAchievedAfterSessions finds goals crossed by a batch of sessions that
// ended together. Seconds are grouped by activity so a bulk stop does not
// miss a threshold reached only by the combined duration.
func (s *Service) NewlyAchievedAfterSessions(ctx context.Context, teamID int64, stoppedSeconds map[int64]int, now time.Time) ([]model.GoalProgress, error) {
	if teamID <= 0 {
		return nil, ErrInvalidTeam
	}
	if now.IsZero() {
		return nil, ErrInvalidProgressQuery
	}
	for activityID, seconds := range stoppedSeconds {
		if activityID <= 0 || seconds < 0 {
			return nil, ErrInvalidProgressQuery
		}
	}
	if len(stoppedSeconds) == 0 {
		return nil, nil
	}
	progress, err := s.Progress(ctx, teamID, now)
	if err != nil {
		return nil, fmt.Errorf("load goal progress after stopped sessions: %w", err)
	}
	var newlyAchieved []model.GoalProgress
	for i := range progress {
		candidate := &progress[i]
		stoppedActivitySeconds, stopped := stoppedSeconds[candidate.Goal.ActivityID]
		if !stopped || candidate.PercentComplete < 100 ||
			candidate.AchievedMinutes-stoppedActivitySeconds/60 >= candidate.Goal.TargetMinutes {
			continue
		}
		newlyAchieved = append(newlyAchieved, *candidate)
	}
	return newlyAchieved, nil
}

// UpsertForManager performs a caller-aware goal write for manager-only routes.
func (s *Service) UpsertForManager(ctx context.Context, request appmodel.GoalUpsertRequest) (model.Goal, error) {
	if request.TeamID <= 0 {
		return model.Goal{}, ErrInvalidTeam
	}
	if request.CallerID <= 0 {
		return model.Goal{}, model.ErrForbidden
	}
	request.ActivityName = strings.TrimSpace(request.ActivityName)
	if request.ActivityName == "" {
		return model.Goal{}, ErrInvalidActivity
	}
	if !validPeriod(request.Period) {
		return model.Goal{}, ErrInvalidPeriod
	}
	if request.Minutes <= 0 {
		return model.Goal{}, ErrInvalidTarget
	}
	goal, err := s.deps.Writes.UpsertGoalForManager(ctx, request)
	if err != nil {
		return model.Goal{}, fmt.Errorf("save goal: %w", err)
	}
	return goal, nil
}

// DeleteForManager performs a caller-aware goal deletion for manager-only routes.
func (s *Service) DeleteForManager(ctx context.Context, request appmodel.GoalDeleteRequest) error {
	if request.TeamID <= 0 {
		return ErrInvalidTeam
	}
	if request.CallerID <= 0 {
		return model.ErrForbidden
	}
	request.ActivityName = strings.TrimSpace(request.ActivityName)
	if request.ActivityName == "" {
		return ErrInvalidActivity
	}
	if !validPeriod(request.Period) {
		return ErrInvalidPeriod
	}
	if err := s.deps.Writes.DeleteGoalForManager(ctx, request); err != nil {
		return fmt.Errorf("delete goal: %w", err)
	}
	return nil
}

func validPeriod(period string) bool {
	switch period {
	case "daily", "weekly", "monthly":
		return true
	default:
		return false
	}
}
