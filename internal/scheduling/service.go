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
	ListSchedule(context.Context, appmodel.ScheduleQuery) ([]appmodel.ScheduleRow, map[int64]string, error)
	PlannedMinutesByUser(ctx context.Context, teamID int64, from, to time.Time) (map[int64]int, error)
	UpsertScheduleEntry(context.Context, appmodel.ScheduleCellRequest) error
	CopyScheduleWeek(context.Context, appmodel.ScheduleCopyRequest) error
	SetScheduleAdminMinutes(context.Context, appmodel.ScheduleAdminRequest) error
	PlannedMinutesForProject(ctx context.Context, teamID, projectID int64) (map[int64]int, error)
	TrackedMinutesForProject(ctx context.Context, teamID, projectID int64, now time.Time) (map[int64]int, error)
	PlannedMinutesForProjectFrom(ctx context.Context, teamID, projectID int64, from time.Time) (int, error)
	SetProjectWeeklyNorm(ctx context.Context, request appmodel.ProjectWeeklyNormRequest) error
	GetProjectWeeklyNorm(ctx context.Context, teamID, projectID int64) (int, bool, error)
	ListProjectWeekTotals(ctx context.Context, teamID, projectID int64, weeks []time.Time, now time.Time) ([]appmodel.ProjectWeekTotal, error)
	AddScheduleLine(ctx context.Context, request appmodel.ScheduleLineRequest) error
	RemoveScheduleLine(ctx context.Context, request appmodel.ScheduleLineRequest) error
}

// ProjectCatalog supplies selectable projects for the schedule editor.
type ProjectCatalog interface {
	List(context.Context, appmodel.ProjectCatalogQuery) ([]model.Project, error)
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

func (s *Service) List(ctx context.Context, query appmodel.ScheduleQuery) (appmodel.ScheduleSnapshot, error) {
	if query.TeamID <= 0 || query.WeekStart.IsZero() {
		return appmodel.ScheduleSnapshot{}, ErrInvalidScheduleCell
	}
	storedRows, names, err := s.store.ListSchedule(ctx, query)
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
		row := Row{ScheduleRow: stored, AvailableMinutes: weekAvailableMinutes(stored.Capacity, stored.AdminMinutes)}
		row.LoadPercent = weekLoadPercent(stored.Total, row.AvailableMinutes)
		row.TrackedPercent = weekLoadPercent(stored.Tracked, row.AvailableMinutes)
		rows = append(rows, row)
	}
	projects, err := s.projects.List(ctx, appmodel.ProjectCatalogQuery{TeamID: query.TeamID})
	if err != nil {
		return appmodel.ScheduleSnapshot{}, fmt.Errorf("list schedule projects: %w", err)
	}
	return appmodel.ScheduleSnapshot{Rows: rows, ProjectNames: names, Projects: projects, TotalMinutes: totalMinutes}, nil
}

// weekAvailableMinutes is the capacity of five working days less the admin
// time the member keeps; never below zero.
func weekAvailableMinutes(capacityPerDay, adminMinutes int) int {
	return max(capacityPerDay*5-adminMinutes, 0)
}

// weekLoadPercent is minutes against the available time of the week. A member
// with nothing available has no load to show, so 0.
func weekLoadPercent(minutes, available int) int {
	if available <= 0 {
		return 0
	}
	return int(int64(minutes) * 100 / int64(available))
}

// CopyScheduleWeek repeats the plan of the previous week in an empty week.
// The rule is the store's: a non-empty target is refused, not merged.
func (s *Service) CopyScheduleWeek(ctx context.Context, request appmodel.ScheduleCopyRequest) error {
	if request.TeamID <= 0 || request.ActorID <= 0 || request.From.IsZero() || request.To.IsZero() ||
		request.To.Format("2006-01-02") != request.From.AddDate(0, 0, 7).Format("2006-01-02") {
		return ErrInvalidScheduleCell
	}
	if err := s.store.CopyScheduleWeek(ctx, request); err != nil {
		return fmt.Errorf("copy schedule week: %w", err)
	}
	return nil
}

// ProjectPlanByMember is each member's planned and tracked minutes on one
// project: the people table on the project page.
func (s *Service) ProjectPlanByMember(ctx context.Context, teamID, projectID int64, now time.Time) (planned, tracked map[int64]int, err error) {
	if teamID <= 0 || projectID <= 0 {
		return nil, nil, ErrInvalidScheduleCell
	}
	planned, err = s.store.PlannedMinutesForProject(ctx, teamID, projectID)
	if err != nil {
		return nil, nil, fmt.Errorf("load project plan: %w", err)
	}
	tracked, err = s.store.TrackedMinutesForProject(ctx, teamID, projectID, now)
	if err != nil {
		return nil, nil, fmt.Errorf("load project tracked time: %w", err)
	}
	return planned, tracked, nil
}

// AddLine puts a project on a person's schedule, with no hours yet.
func (s *Service) AddLine(ctx context.Context, request appmodel.ScheduleLineRequest) error {
	if request.TeamID <= 0 || request.ActorID <= 0 || request.UserID <= 0 || request.ProjectID <= 0 {
		return ErrInvalidScheduleCell
	}
	if err := s.store.AddScheduleLine(ctx, request); err != nil {
		return fmt.Errorf("add schedule line: %w", err)
	}
	return nil
}

// RemoveLine takes a project off a person's schedule, if it carries no hours.
func (s *Service) RemoveLine(ctx context.Context, request appmodel.ScheduleLineRequest) error {
	if request.TeamID <= 0 || request.ActorID <= 0 || request.UserID <= 0 || request.ProjectID <= 0 {
		return ErrInvalidScheduleCell
	}
	if err := s.store.RemoveScheduleLine(ctx, request); err != nil {
		return fmt.Errorf("remove schedule line: %w", err)
	}
	return nil
}

// PlannedAhead is the plan on a project from a day onward, in minutes.
func (s *Service) PlannedAhead(ctx context.Context, teamID, projectID int64, from time.Time) (int, error) {
	if teamID <= 0 || projectID <= 0 {
		return 0, ErrInvalidScheduleCell
	}
	minutes, err := s.store.PlannedMinutesForProjectFrom(ctx, teamID, projectID, from)
	if err != nil {
		return 0, fmt.Errorf("load plan ahead: %w", err)
	}
	return minutes, nil
}

// SetScheduleAdminMinutes stores a member's weekly admin time, between zero
// and a full week.
func (s *Service) SetScheduleAdminMinutes(ctx context.Context, request appmodel.ScheduleAdminRequest) error {
	if request.TeamID <= 0 || request.ActorID <= 0 || request.UserID <= 0 || request.Minutes < 0 || request.Minutes > 7*24*60 {
		return ErrInvalidScheduleCell
	}
	if err := s.store.SetScheduleAdminMinutes(ctx, request); err != nil {
		return fmt.Errorf("save schedule admin time: %w", err)
	}
	return nil
}

// PlannedMinutesByUser is each member's planned minutes over the days in
// [from, to), for the reports that compare the plan with tracked time.
func (s *Service) PlannedMinutesByUser(ctx context.Context, teamID int64, from, to time.Time) (map[int64]int, error) {
	if teamID <= 0 {
		return nil, ErrInvalidScheduleCell
	}
	planned, err := s.store.PlannedMinutesByUser(ctx, teamID, from, to)
	if err != nil {
		return nil, fmt.Errorf("load planned minutes: %w", err)
	}
	return planned, nil
}

func (s *Service) SetCell(ctx context.Context, request appmodel.ScheduleCellRequest) error {
	if request.TeamID <= 0 || request.ActorID <= 0 || request.UserID <= 0 || request.ProjectID < 0 || request.Day.IsZero() || request.Minutes < 0 || request.Minutes > 24*60 {
		return ErrInvalidScheduleCell
	}
	if request.ProjectID == 0 {
		projects, err := s.projects.List(ctx, appmodel.ProjectCatalogQuery{TeamID: request.TeamID})
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

// ProjectWeekTotals is the plan and the tracked time on one project for each of
// the given weeks, by the first day of each week (PLAN-02).
func (s *Service) ProjectWeekTotals(ctx context.Context, teamID, projectID int64, weeks []time.Time, now time.Time) ([]appmodel.ProjectWeekTotal, error) {
	if teamID <= 0 || projectID <= 0 {
		return nil, ErrInvalidScheduleCell
	}
	totals, err := s.store.ListProjectWeekTotals(ctx, teamID, projectID, weeks, now)
	if err != nil {
		return nil, fmt.Errorf("load project weeks: %w", err)
	}
	return totals, nil
}

// SetProjectWeeklyNorm stores a project's weekly norm; zero minutes removes it.
// Only a manager may set it, and the store checks that under its own lock.
func (s *Service) SetProjectWeeklyNorm(ctx context.Context, request appmodel.ProjectWeeklyNormRequest) error {
	if request.TeamID <= 0 || request.ProjectID <= 0 || request.CallerID <= 0 || request.Minutes < 0 || request.Minutes > 7*24*60 {
		return ErrInvalidScheduleCell
	}
	if err := s.store.SetProjectWeeklyNorm(ctx, request); err != nil {
		return fmt.Errorf("save project weekly norm: %w", err)
	}
	return nil
}

// ProjectWeeklyNorm reads a project's weekly norm in minutes; ok is false when the
// project has none.
func (s *Service) ProjectWeeklyNorm(ctx context.Context, teamID, projectID int64) (minutes int, ok bool, err error) {
	if teamID <= 0 || projectID <= 0 {
		return 0, false, ErrInvalidScheduleCell
	}
	minutes, ok, err = s.store.GetProjectWeeklyNorm(ctx, teamID, projectID)
	if err != nil {
		return 0, false, fmt.Errorf("load project weekly norm: %w", err)
	}
	return minutes, ok, nil
}
