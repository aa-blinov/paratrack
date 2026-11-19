package appmodel

// ReportGroup selects the key used to combine tracked time in a report.
type ReportGroup string

const (
	ReportGroupProject  ReportGroup = "project"
	ReportGroupActivity ReportGroup = "activity"
	ReportGroupUser     ReportGroup = "user"
	ReportGroupDay      ReportGroup = "day"
	// ReportGroupDayPerson splits each day by member (PLAN-07).
	ReportGroupDayPerson ReportGroup = "day-person"
	// ReportGroupTask groups by the task an activity belongs to (UX-16): the
	// answer to "what did this task cost" that tasks and money never gave
	// together. Time on activities without a task keeps its activity name.
	ReportGroupTask ReportGroup = "task"
)

func (group ReportGroup) Valid() bool {
	switch group {
	case ReportGroupProject, ReportGroupActivity, ReportGroupUser, ReportGroupDay, ReportGroupDayPerson, ReportGroupTask:
		return true
	default:
		return false
	}
}
