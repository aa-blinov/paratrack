package scheduling

import (
	"context"
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
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
	rows, _, err := service.List(context.Background(), 7, time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("list schedule: %v", err)
	}
	if rows[0].LoadPercent != 50 {
		t.Fatalf("weekly load = %d%%, want 50%%", rows[0].LoadPercent)
	}
	if rows[1].LoadPercent != 0 {
		t.Fatalf("load with no configured capacity = %d%%, want 0%%", rows[1].LoadPercent)
	}
}
