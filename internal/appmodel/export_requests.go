package appmodel

import "time"

type ExportBuildQuery struct {
	TeamID  int64
	Start   time.Time
	End     time.Time
	Now     time.Time
	HasFrom bool
	HasTo   bool
}

type ExportSnapshot struct {
	Rows []ExportRow
}

type ExportRow struct {
	SessionID       int64
	ActivityName    string
	ProjectName     string
	StartAt         time.Time
	EndAt           *time.Time
	DurationSeconds int
	Note            string
}
