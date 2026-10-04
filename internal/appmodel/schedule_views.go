package appmodel

import "github.com/aa-blinov/paratrack/internal/model"

// ScheduleRow is one member's planned work during a week.
type ScheduleRow struct {
	UserID    int64
	UserName  string
	Capacity  int
	Minutes   [7]int
	Total     int
	ByProject map[int64][7]int
}

type ScheduleViewRow struct {
	ScheduleRow
	LoadPercent int
}

type ScheduleSnapshot struct {
	Rows         []ScheduleViewRow
	ProjectNames map[int64]string
	Projects     []model.Project
	TotalMinutes int
}
