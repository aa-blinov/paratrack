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
)

func (group ReportGroup) Valid() bool {
	switch group {
	case ReportGroupProject, ReportGroupActivity, ReportGroupUser, ReportGroupDay, ReportGroupDayPerson:
		return true
	default:
		return false
	}
}
