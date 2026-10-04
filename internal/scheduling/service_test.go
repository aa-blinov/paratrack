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
	rows []model.ScheduleRow
}

type scheduleProjectCatalogStub struct {
	projects        []model.Project
	teamID          int64
	includeArchived bool
}

func (s *scheduleProjectCatalogStub) List(_ context.Context, teamID int64, includeArchived bool) ([]model.Project, error) {
	s.teamID, s.includeArchived = teamID, includeArchived
	return s.projects, nil
}

func (s scheduleStoreStub) ListSchedule(context.Context, int64, time.Time) ([]model.ScheduleRow, map[int64]string, error) {
	return s.rows, nil, nil
}
func (scheduleStoreStub) UpsertScheduleEntry(context.Context, appmodel.ScheduleCellRequest) error {
	return nil
}

func TestListCalculatesWeeklyLoadInSchedulingWorkflow(t *testing.T) {
	projects := &scheduleProjectCatalogStub{projects: []model.Project{{ID: 9, Name: "Alpha"}}}
	service, err := New(Dependencies{Store: scheduleStoreStub{rows: []model.ScheduleRow{
		{UserID: 1, Capacity: 60, Total: 150},
		{UserID: 2, Capacity: 0, Total: 90},
	}}, Projects: projects})
	if err != nil {
		t.Fatalf("construct scheduling service: %v", err)
	}
	snapshot, err := service.List(context.Background(), 7, time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("list schedule: %v", err)
	}
	if snapshot.Rows[0].LoadPercent != 50 {
		t.Fatalf("weekly load = %d%%, want 50%%", snapshot.Rows[0].LoadPercent)
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
	service, err := New(Dependencies{Store: scheduleStoreStub{rows: []model.ScheduleRow{{Total: maxInt}, {Total: 1}}}, Projects: &scheduleProjectCatalogStub{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.List(context.Background(), 7, time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC))
	if !errors.Is(err, money.ErrOverflow) {
		t.Fatalf("List() overflow error = %v, want %v", err, money.ErrOverflow)
	}
}
