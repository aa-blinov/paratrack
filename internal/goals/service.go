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
	UpsertGoalsForManager(context.Context, appmodel.GoalSetRequest) ([]model.Goal, error)
	DeleteGoalsForManager(context.Context, appmodel.GoalUnsetRequest) (int, error)
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
	goals, err := s.SetForManager(ctx, appmodel.GoalSetRequest{
		TeamID: request.TeamID, CallerID: request.CallerID, ActivityName: request.ActivityName,
		Targets: []appmodel.GoalTarget{{Period: request.Period, Minutes: request.Minutes}},
	})
	if err != nil {
		return model.Goal{}, err
	}
	if len(goals) != 1 {
		return model.Goal{}, fmt.Errorf("save goal: expected one result, got %d", len(goals))
	}
	return goals[0], nil
}

// SetForManager validates all targets before asking persistence to apply them
// atomically under the caller's current manager role.
func (s *Service) SetForManager(ctx context.Context, request appmodel.GoalSetRequest) ([]model.Goal, error) {
	if request.TeamID <= 0 {
		return nil, ErrInvalidTeam
	}
	if request.CallerID <= 0 {
		return nil, model.ErrForbidden
	}
	request.ActivityName = strings.TrimSpace(request.ActivityName)
	if request.ActivityName == "" {
		return nil, ErrInvalidActivity
	}
	if len(request.Targets) == 0 {
		return nil, ErrInvalidPeriod
	}
	seen := make(map[string]struct{}, len(request.Targets))
	for _, target := range request.Targets {
		if !validPeriod(target.Period) {
			return nil, ErrInvalidPeriod
		}
		if target.Minutes <= 0 {
			return nil, ErrInvalidTarget
		}
		if _, exists := seen[target.Period]; exists {
			return nil, ErrInvalidPeriod
		}
		seen[target.Period] = struct{}{}
	}
	goals, err := s.deps.Writes.UpsertGoalsForManager(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("save goals: %w", err)
	}
	return goals, nil
}

// DeleteForManager performs a caller-aware goal deletion for manager-only routes.
func (s *Service) DeleteForManager(ctx context.Context, request appmodel.GoalDeleteRequest) error {
	if request.TeamID <= 0 {
		return ErrInvalidTeam
	}
	deleted, err := s.UnsetForManager(ctx, appmodel.GoalUnsetRequest{
		TeamID: request.TeamID, CallerID: request.CallerID, ActivityName: request.ActivityName,
		Periods: []string{request.Period},
	})
	if err != nil {
		return err
	}
	if deleted == 0 {
		return model.ErrGoalNotFound
	}
	return nil
}

// UnsetForManager deletes selected goal periods in one persistence
// transaction. Missing periods are tolerated for multi-period CLI cleanup.
func (s *Service) UnsetForManager(ctx context.Context, request appmodel.GoalUnsetRequest) (int, error) {
	if request.TeamID <= 0 {
		return 0, ErrInvalidTeam
	}
	if request.CallerID <= 0 {
		return 0, model.ErrForbidden
	}
	request.ActivityName = strings.TrimSpace(request.ActivityName)
	if request.ActivityName == "" {
		return 0, ErrInvalidActivity
	}
	if len(request.Periods) == 0 {
		return 0, ErrInvalidPeriod
	}
	seen := make(map[string]struct{}, len(request.Periods))
	for _, period := range request.Periods {
		if !validPeriod(period) {
			return 0, ErrInvalidPeriod
		}
		if _, exists := seen[period]; exists {
			return 0, ErrInvalidPeriod
		}
		seen[period] = struct{}{}
	}
	deleted, err := s.deps.Writes.DeleteGoalsForManager(ctx, request)
	if err != nil {
		return 0, fmt.Errorf("delete goals: %w", err)
	}
	return deleted, nil
}

func validPeriod(period string) bool {
	switch period {
	case "daily", "weekly", "monthly":
		return true
	default:
		return false
	}
}
