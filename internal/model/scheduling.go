package model

// ScheduleRow is one member's planned work during a week.
type ScheduleRow struct {
	UserID    int64
	UserName  string
	Capacity  int
	Minutes   [7]int
	Total     int
	ByProject map[int64][7]int
}
