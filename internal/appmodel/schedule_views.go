package appmodel

import "github.com/aa-blinov/paratrack/internal/model"

type ScheduleViewRow struct {
	model.ScheduleRow
	LoadPercent int
}

type ScheduleSnapshot struct {
	Rows         []ScheduleViewRow
	ProjectNames map[int64]string
	Projects     []model.Project
	TotalMinutes int
}
