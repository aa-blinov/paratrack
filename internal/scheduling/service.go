// Package scheduling owns team planning reads and schedule-cell rules.
package scheduling

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
)

type Store interface {
	ListSchedule(context.Context, int64, time.Time) ([]model.ScheduleRow, map[int64]string, error)
	UpsertScheduleEntry(context.Context, appmodel.ScheduleCellRequest) error
}

// ProjectCatalog supplies selectable projects for the schedule editor.
type ProjectCatalog interface {
	List(context.Context, int64, bool) ([]model.Project, error)
}

// Row adds scheduling policy derived from the persisted weekly plan.
type Row = appmodel.ScheduleViewRow

type Dependencies struct {
	Store    Store
	Projects ProjectCatalog
}

type Service struct {
	store    Store
	projects ProjectCatalog
}

var ErrIncompleteDependencies = errors.New("scheduling service dependencies are incomplete")

func New(deps Dependencies) (*Service, error) {
	if depcheck.IsNil(deps.Store) || depcheck.IsNil(deps.Projects) {
		return nil, ErrIncompleteDependencies
	}
	return &Service{store: deps.Store, projects: deps.Projects}, nil
}

var ErrInvalidScheduleCell = appmodel.ErrInvalidScheduleCell
var ErrNoProjects = appmodel.ErrNoScheduleProjects

func (s *Service) List(ctx context.Context, teamID int64, weekStart time.Time) (appmodel.ScheduleSnapshot, error) {
	if teamID <= 0 || weekStart.IsZero() {
		return appmodel.ScheduleSnapshot{}, ErrInvalidScheduleCell
	}
	storedRows, names, err := s.store.ListSchedule(ctx, teamID, weekStart)
	if err != nil {
		return appmodel.ScheduleSnapshot{}, fmt.Errorf("list weekly schedule: %w", err)
	}
	rows := make([]Row, 0, len(storedRows))
	totalMinutes := 0
	for _, stored := range storedRows {
		totalMinutes, err = money.AddInt(totalMinutes, stored.Total)
		if err != nil {
			return appmodel.ScheduleSnapshot{}, fmt.Errorf("sum weekly schedule minutes: %w", err)
		}
		row := Row{ScheduleRow: stored}
		if stored.Capacity > 0 {
			row.LoadPercent = int(int64(stored.Total) * 100 / (int64(stored.Capacity) * 5))
		}
		rows = append(rows, row)
	}
	projects, err := s.projects.List(ctx, teamID, false)
	if err != nil {
		return appmodel.ScheduleSnapshot{}, fmt.Errorf("list schedule projects: %w", err)
	}
	return appmodel.ScheduleSnapshot{Rows: rows, ProjectNames: names, Projects: projects, TotalMinutes: totalMinutes}, nil
}

func (s *Service) SetCell(ctx context.Context, request appmodel.ScheduleCellRequest) error {
	if request.TeamID <= 0 || request.ActorID <= 0 || request.UserID <= 0 || request.ProjectID < 0 || request.Day.IsZero() || request.Minutes < 0 || request.Minutes > 24*60 {
		return ErrInvalidScheduleCell
	}
	if request.ProjectID == 0 {
		projects, err := s.projects.List(ctx, request.TeamID, false)
		if err != nil {
			return fmt.Errorf("resolve default schedule project: %w", err)
		}
		if len(projects) == 0 {
			return ErrNoProjects
		}
		request.ProjectID = projects[0].ID
		if request.ProjectID <= 0 {
			return ErrInvalidScheduleCell
		}
	}
	request.Day = time.Date(request.Day.Year(), request.Day.Month(), request.Day.Day(), 0, 0, 0, 0, time.UTC)
	if err := s.store.UpsertScheduleEntry(ctx, request); err != nil {
		return fmt.Errorf("save schedule cell: %w", err)
	}
	return nil
}
