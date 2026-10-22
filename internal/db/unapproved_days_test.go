package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// Only billable time that is not on an invoice and sits outside every approved
// week is listed, and it is listed by day (FIN-12). Time in an approved week or on
// a project with no rate stays out of the warning, as it stays out of the invoice.
func TestListUnapprovedTimeDaysKeepsOnlyInvoiceableTime(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Недели", "unapproved-days")
	ownerID := teamOwner(t, d, teamID)
	ctx = requestctx.WithActor(ctx, ownerID)
	billable, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Сайт", Color: "#7c3aed"})
	if err != nil {
		t.Fatalf("create billable project: %v", err)
	}
	rate := 10000
	if err := d.SetProjectRate(ctx, appmodel.ProjectRateRequest{TeamID: teamID, ProjectID: billable.ID, CallerID: ownerID, RateCents: &rate}); err != nil {
		t.Fatalf("rate: %v", err)
	}
	unrated, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: teamID, CallerID: ownerID, Name: "Без ставки", Color: "#16a34a"})
	if err != nil {
		t.Fatalf("create unrated project: %v", err)
	}
	activity := func(name string, projectID int64) int64 {
		t.Helper()
		id := newTestActivity(t, d, ctx, teamID, ownerID, name)
		if err := d.AssignActivityProject(ctx, appmodel.AssignActivityProjectRequest{TeamID: teamID, ActivityID: id, ProjectID: projectID, CallerID: ownerID}); err != nil {
			t.Fatalf("assign activity: %v", err)
		}
		return id
	}
	paid := activity("сайт", billable.ID)
	free := activity("бесплатно", unrated.ID)
	session := func(activityID int64, at string) {
		t.Helper()
		start, err := time.Parse("2006-01-02T15:04", at)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activityID, Start: start, End: start.Add(time.Hour)}); err != nil {
			t.Fatalf("session %s: %v", at, err)
		}
	}
	session(paid, "2026-10-06T09:00")
	session(paid, "2026-10-12T09:00")
	session(paid, "2026-10-14T09:00")
	session(free, "2026-10-13T09:00")
	if err := d.ApproveTimesheetWeek(ctx, appmodel.TimesheetApprovalRequest{
		TeamID: teamID, CallerID: ownerID,
		Start: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("approve week: %v", err)
	}

	days, err := d.ListUnapprovedTimeDays(ctx, teamID, 0,
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("list unapproved days: %v", err)
	}
	if len(days) != 2 || days[0] != "2026-10-12" || days[1] != "2026-10-14" {
		t.Fatalf("unapproved days = %v, want [2026-10-12 2026-10-14]", days)
	}
}
