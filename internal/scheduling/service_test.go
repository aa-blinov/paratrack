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

func (s scheduleStoreStub) ListSchedule(context.Context, int64, time.Time) ([]model.ScheduleRow, map[int64]string, error) {
	return s.rows, nil, nil
}
func (scheduleStoreStub) UpsertScheduleEntry(context.Context, appmodel.ScheduleCellRequest) error {
	return nil
}

func TestListCalculatesWeeklyLoadInSchedulingWorkflow(t *testing.T) {
	service, err := New(scheduleStoreStub{rows: []model.ScheduleRow{
		{UserID: 1, Capacity: 60, Total: 150},
		{UserID: 2, Capacity: 0, Total: 90},
	}})
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
}

func TestListRejectsOverflowInTeamTotal(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	service, err := New(scheduleStoreStub{rows: []model.ScheduleRow{{Total: maxInt}, {Total: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.List(context.Background(), 7, time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC))
	if !errors.Is(err, money.ErrOverflow) {
		t.Fatalf("List() overflow error = %v, want %v", err, money.ErrOverflow)
	}
}
