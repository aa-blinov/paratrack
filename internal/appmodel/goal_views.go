package appmodel

import (
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

// GoalProgress combines a goal with the activity's progress in its current
// period.
type GoalProgress struct {
	Goal            model.Goal
	ActivityName    string
	AchievedMinutes int
	PercentComplete int
	PeriodStart     time.Time
	PeriodEnd       time.Time
}
