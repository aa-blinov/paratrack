package appmodel

// ReportGroup selects the key used to combine tracked time in a report.
type ReportGroup string

const (
	ReportGroupProject  ReportGroup = "project"
	ReportGroupActivity ReportGroup = "activity"
	ReportGroupUser     ReportGroup = "user"
	ReportGroupDay      ReportGroup = "day"
)

func (group ReportGroup) Valid() bool {
	switch group {
	case ReportGroupProject, ReportGroupActivity, ReportGroupUser, ReportGroupDay:
		return true
	default:
		return false
	}
}
