package appmodel

import (
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

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

// MemberPayrollQuery scopes one person's own pay lines to one workspace (FIN-11).
type MemberPayrollQuery struct {
	TeamID int64
	UserID int64
}

// MemberPayrollLine is one of a person's lines on a pay run, as they see it: the run,
// its period (end exclusive, as stored) and status, and what the line came to.
type MemberPayrollLine struct {
	RunNumber   string
	RunStatus   string
	PeriodStart time.Time
	PeriodEnd   time.Time
	Currency    string
	Seconds     int
	RateCents   int
	AmountCents int
}
