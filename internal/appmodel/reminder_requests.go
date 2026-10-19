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

// OverloadNoticeRequest claims the one overload notice a person gets in a week.
type OverloadNoticeRequest struct {
	TeamID int64
	UserID int64
	Week   string
}
