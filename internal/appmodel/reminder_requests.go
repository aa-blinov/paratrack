package appmodel

// DueProject is a project whose deadline falls in the reminder window.
type DueProject struct {
	TeamID     int64
	ProjectID  int64
	Name       string
	Slug       string
	Deadline   string
	LeadUserID int64
}

// DeadlineReminderRequest records that the reminder for a project's deadline
// went out.
type DeadlineReminderRequest struct {
	TeamID    int64
	ProjectID int64
	Deadline  string
}

// DueTask is an open, assigned task with a due date: a candidate for a due-date
// reminder. Reminded holds the marker of the last reminder it got (TASK-05).
type DueTask struct {
	TeamID     int64
	TaskID     int64
	Title      string
	Due        string
	AssigneeID int64
	Reminded   string
}

// TaskDueReminderRequest records which reminder a task got for its due date.
type TaskDueReminderRequest struct {
	TeamID int64
	TaskID int64
	Marker string
}

// OverloadNoticeRequest claims the one overload notice a person gets in a week.
type OverloadNoticeRequest struct {
	TeamID int64
	UserID int64
	Week   string
}
