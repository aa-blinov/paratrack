package scheduling

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
)

type scheduleStoreStub struct {
	rows  []appmodel.ScheduleRow
	query appmodel.ScheduleQuery
}

type scheduleCellStoreStub struct {
	request appmodel.ScheduleCellRequest
}

func (scheduleCellStoreStub) ListSchedule(context.Context, appmodel.ScheduleQuery) ([]appmodel.ScheduleRow, map[int64]string, error) {
	return nil, nil, nil
}

func (scheduleCellStoreStub) PlannedMinutesByUser(context.Context, int64, time.Time, time.Time) (map[int64]int, error) {
	return nil, nil
}

func (scheduleCellStoreStub) CopyScheduleWeek(context.Context, appmodel.ScheduleCopyRequest) error {
	return nil
}

func (scheduleCellStoreStub) SetScheduleAdminMinutes(context.Context, appmodel.ScheduleAdminRequest) error {
	return nil
}

func (scheduleCellStoreStub) PlannedMinutesForProject(context.Context, int64, int64) (map[int64]int, error) {
	return nil, nil
}

func (scheduleCellStoreStub) TrackedMinutesForProject(context.Context, int64, int64, time.Time) (map[int64]int, error) {
	return nil, nil
}

func (scheduleCellStoreStub) PlannedMinutesForProjectFrom(context.Context, int64, int64, time.Time) (int, error) {
	return 0, nil
}

func (scheduleCellStoreStub) AddScheduleLine(context.Context, appmodel.ScheduleLineRequest) error {
	return nil
}

func (scheduleCellStoreStub) RemoveScheduleLine(context.Context, appmodel.ScheduleLineRequest) error {
	return nil
}
func (s *scheduleCellStoreStub) UpsertScheduleEntry(_ context.Context, request appmodel.ScheduleCellRequest) error {
	s.request = request
	return nil
}

type scheduleProjectCatalogStub struct {
	projects        []model.Project
	teamID          int64
	includeArchived bool
}

func (s *scheduleProjectCatalogStub) List(_ context.Context, query appmodel.ProjectCatalogQuery) ([]model.Project, error) {
	s.teamID, s.includeArchived = query.TeamID, query.IncludeArchived
	return s.projects, nil
}

func (s *scheduleStoreStub) ListSchedule(_ context.Context, query appmodel.ScheduleQuery) ([]appmodel.ScheduleRow, map[int64]string, error) {
	s.query = query
	return s.rows, nil, nil
}
func (scheduleStoreStub) UpsertScheduleEntry(context.Context, appmodel.ScheduleCellRequest) error {
	return nil
}

func (scheduleStoreStub) PlannedMinutesByUser(context.Context, int64, time.Time, time.Time) (map[int64]int, error) {
	return map[int64]int{1: 240}, nil
}

func (scheduleStoreStub) CopyScheduleWeek(context.Context, appmodel.ScheduleCopyRequest) error {
	return nil
}

func (scheduleStoreStub) SetScheduleAdminMinutes(context.Context, appmodel.ScheduleAdminRequest) error {
	return nil
}

func (scheduleStoreStub) PlannedMinutesForProject(context.Context, int64, int64) (map[int64]int, error) {
	return nil, nil
}

func (scheduleStoreStub) TrackedMinutesForProject(context.Context, int64, int64, time.Time) (map[int64]int, error) {
	return nil, nil
}

func (scheduleStoreStub) PlannedMinutesForProjectFrom(context.Context, int64, int64, time.Time) (int, error) {
	return 0, nil
}

func (scheduleStoreStub) AddScheduleLine(context.Context, appmodel.ScheduleLineRequest) error {
	return nil
}

func (scheduleStoreStub) RemoveScheduleLine(context.Context, appmodel.ScheduleLineRequest) error {
	return nil
}
func TestListCalculatesWeeklyLoadInSchedulingWorkflow(t *testing.T) {
	projects := &scheduleProjectCatalogStub{projects: []model.Project{{ID: 9, Name: "Alpha"}}}
	store := &scheduleStoreStub{rows: []appmodel.ScheduleRow{
		{UserID: 1, Capacity: 60, Total: 150, Tracked: 225, AdminMinutes: 60},
		{UserID: 2, Capacity: 0, Total: 90},
	}}
	service, err := New(Dependencies{Store: store, Projects: projects})
	if err != nil {
		t.Fatalf("construct scheduling service: %v", err)
	}
	query := appmodel.ScheduleQuery{TeamID: 7, WeekStart: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)}
	snapshot, err := service.List(context.Background(), query)
	if err != nil {
		t.Fatalf("list schedule: %v", err)
	}
	// Five days of 60 minutes less one hour of admin leaves 240 available: the
	// load is measured against what is left for clients, not against 300.
	if snapshot.Rows[0].AvailableMinutes != 240 {
		t.Fatalf("available = %d minutes, want 240 (60*5 less 60 admin)", snapshot.Rows[0].AvailableMinutes)
	}
	if snapshot.Rows[0].LoadPercent != 62 {
		t.Fatalf("weekly load = %d%%, want 62%% (150 of 240)", snapshot.Rows[0].LoadPercent)
	}
	if snapshot.Rows[0].TrackedPercent != 93 {
		t.Fatalf("tracked load = %d%%, want 93%% (225 of 240)", snapshot.Rows[0].TrackedPercent)
	}
	if store.query.TeamID != query.TeamID || !store.query.WeekStart.Equal(query.WeekStart) {
		t.Fatalf("store schedule query = %+v, want %+v", store.query, query)
	}
	if snapshot.Rows[1].LoadPercent != 0 {
		t.Fatalf("load with no configured capacity = %d%%, want 0%%", snapshot.Rows[1].LoadPercent)
	}
	if snapshot.TotalMinutes != 240 {
		t.Fatalf("weekly team total = %d minutes, want 240", snapshot.TotalMinutes)
	}
	if projects.teamID != 7 || projects.includeArchived || len(snapshot.Projects) != 1 || snapshot.Projects[0].ID != 9 {
		t.Fatalf("schedule projects = %+v, catalog query team=%d archived=%v", snapshot.Projects, projects.teamID, projects.includeArchived)
	}
}

func TestListRejectsOverflowInTeamTotal(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	service, err := New(Dependencies{Store: &scheduleStoreStub{rows: []appmodel.ScheduleRow{{Total: maxInt}, {Total: 1}}}, Projects: &scheduleProjectCatalogStub{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.List(context.Background(), appmodel.ScheduleQuery{TeamID: 7, WeekStart: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)})
	if !errors.Is(err, money.ErrOverflow) {
		t.Fatalf("List() overflow error = %v, want %v", err, money.ErrOverflow)
	}
}

func TestSetCellResolvesDefaultProjectInWorkflow(t *testing.T) {
	store := &scheduleCellStoreStub{}
	projects := &scheduleProjectCatalogStub{projects: []model.Project{{ID: 9}, {ID: 10}}}
	service, err := New(Dependencies{Store: store, Projects: projects})
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 10, 7, 14, 30, 0, 0, time.FixedZone("UTC+3", 3*60*60))
	err = service.SetCell(context.Background(), appmodel.ScheduleCellRequest{
		TeamID: 7, ActorID: 8, UserID: 11, Day: day, Minutes: 30,
	})
	if err != nil {
		t.Fatalf("set cell with default project: %v", err)
	}
	if store.request.ProjectID != 9 || store.request.Day.Location() != time.UTC || store.request.Day.Hour() != 0 || store.request.Day.Minute() != 0 {
		t.Fatalf("persisted schedule request = %+v, want first project and UTC midnight", store.request)
	}
	if projects.teamID != 7 || projects.includeArchived {
		t.Fatalf("project lookup team=%d includeArchived=%v", projects.teamID, projects.includeArchived)
	}
}

func TestSetCellWithoutProjectsReturnsApplicationOutcome(t *testing.T) {
	service, err := New(Dependencies{Store: &scheduleCellStoreStub{}, Projects: &scheduleProjectCatalogStub{}})
	if err != nil {
		t.Fatal(err)
	}
	err = service.SetCell(context.Background(), appmodel.ScheduleCellRequest{
		TeamID: 7, ActorID: 8, UserID: 11, Day: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), Minutes: 30,
	})
	if !errors.Is(err, appmodel.ErrNoScheduleProjects) {
		t.Fatalf("SetCell() error = %v, want %v", err, appmodel.ErrNoScheduleProjects)
	}
}
