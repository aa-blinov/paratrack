package appmodel

import "github.com/aa-blinov/paratrack/internal/model"

// SessionStoppedNotification describes the post-commit notification for a
// timer transition without coupling the tracking workflow to a delivery
// provider.
type SessionStoppedNotification struct {
	TeamID       int64
	UserID       int64
	ActivityName string
}

// GoalAchievedNotification carries the goal result and recipient to the
// notification adapter after a successful tracking transition.
type GoalAchievedNotification struct {
	TeamID int64
	UserID int64
	Goal   model.GoalProgress
}

// PayrollPaidNotification identifies recipients of a completed payroll run.
type PayrollPaidNotification struct {
	TeamID     int64
	Recipients []int64
}
