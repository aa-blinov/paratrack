package appmodel

import "github.com/aa-blinov/paratrack/internal/model"

type PayrollRunSummary struct {
	Run                  model.PayrollRun
	TotalCents           int
	TotalHoursHundredths int
}

type PayrollRunDetail struct {
	Run                  model.PayrollRun
	Lines                []model.PayrollLine
	TotalCents           int
	TotalHoursHundredths int
}

// PayrollRunDetails joins one immutable payroll run with its line snapshot.
type PayrollRunDetails struct {
	Run   model.PayrollRun
	Lines []model.PayrollLine
}
