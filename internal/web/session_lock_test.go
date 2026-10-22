package web

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// A session belongs to the approved week it starts in, and the week's end is
// excluded (UI-05). A session outside every span is not marked.
func TestSessionStartsInAnApprovedWeek(t *testing.T) {
	week := appmodel.TimesheetApproval{
		Start: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC),
	}
	approvals := []appmodel.TimesheetApproval{week}
	if _, ok := approvedWeekOf(approvals, time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)); !ok {
		t.Error("a session on Wednesday of an approved week is not marked")
	}
	if _, ok := approvedWeekOf(approvals, time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)); ok {
		t.Error("the first moment after the approved week is marked, but its end is excluded")
	}
	if _, ok := approvedWeekOf(approvals, time.Date(2026, 10, 11, 23, 0, 0, 0, time.UTC)); ok {
		t.Error("a session before the approved week is marked")
	}
}
