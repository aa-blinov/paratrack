package payroll

import (
	"context"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// The list names the people each run prices (UX-44): three runs of one period
// are told apart by who is in them, not just by the document number.
func TestListRunsNamesTheMembers(t *testing.T) {
	build := func(lines ...model.PayrollLine) []appmodel.PayrollRunSummary {
		store := &payrollClockStore{runs: []appmodel.PayrollRunDetails{{
			Run:   model.PayrollRun{ID: 5, Number: "PAY-2026-W22"},
			Lines: lines,
		}}}
		service, err := NewServiceWithClock(store, time.Now, payrollAuditNoop{}, payrollLoggerNoop{})
		if err != nil {
			t.Fatalf("construct service: %v", err)
		}
		summaries, err := service.ListRuns(context.Background(), appmodel.PayrollRunListQuery{TeamID: 1})
		if err != nil {
			t.Fatalf("list runs: %v", err)
		}
		return summaries
	}

	two := build(
		model.PayrollLine{UserID: 1, Label: "Маша"},
		model.PayrollLine{UserID: 2, Label: "Петя"},
	)
	if len(two) != 1 || len(two[0].Members) != 2 || two[0].Members[0] != "Маша" {
		t.Fatalf("members = %+v, want Маша then Петя named once each", two)
	}
	// A run prices one person across two activities: the line labels carry the
	// person's name, so it is said once.
	one := build(
		model.PayrollLine{UserID: 1, Label: "Маша"},
		model.PayrollLine{UserID: 1, Label: "Маша"},
	)
	if len(one[0].Members) != 1 {
		t.Fatalf("members = %+v, want the person named once", one[0].Members)
	}
}
