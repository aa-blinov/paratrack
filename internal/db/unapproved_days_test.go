package db

import (
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// Only billable time that is not on an invoice and sits outside every approved
// week is listed, and it is listed by the first day of its week (FIN-12). Time
// in an approved week or on a project with no rate stays out of the warning, as
// it stays out of the invoice.
func TestListUnapprovedTimeWeeksKeepsOnlyInvoiceableTime(t *testing.T) {
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

	weeks, err := d.ListUnapprovedTimeWeeks(ctx, teamID, 0,
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("list unapproved weeks: %v", err)
	}
	// The 12th (a Monday) and the 14th (a Wednesday) are one Monday-first
	// week in UTC; the unrated project's time and the approved week stay out.
	if len(weeks) != 1 || weeks[0] != "2026-10-12" {
		t.Fatalf("unapproved weeks = %v, want [2026-10-12]", weeks)
	}
}

// The week is cut in the workspace's own zone and from its own first weekday
// (ADM-07), so the warning names a week the manager can actually approve.
func TestListUnapprovedTimeWeeksFollowTheWorkspaceZoneAndWeekStart(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Недели зоны", "unapproved-weeks-zone")
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
	var activityID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO activities (name, team_id) VALUES ('Зона', ?) RETURNING id`, teamID).Scan(&activityID); err != nil {
		t.Fatal(err)
	}
	if err := d.AssignActivityProject(ctx, appmodel.AssignActivityProjectRequest{TeamID: teamID, ActivityID: activityID, ProjectID: billable.ID, CallerID: ownerID}); err != nil {
		t.Fatal(err)
	}
	// 2026-10-11 is a Sunday. 21:30 UTC is already Monday the 12th in
	// Moscow, so a Moscow workspace that starts its weeks on Sunday names
	// the week of the 11th, while the UTC default names the week of the 5th.
	start, err := time.Parse("2006-01-02T15:04", "2026-10-11T21:30")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateClosedSession(ctx, appmodel.TimerAddRequest{TeamID: teamID, ActivityID: activityID, Start: start, End: start.Add(time.Hour)}); err != nil {
		t.Fatalf("session: %v", err)
	}
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	weeks, err := d.ListUnapprovedTimeWeeks(ctx, teamID, 0, from, to)
	if err != nil {
		t.Fatalf("list in the default zone: %v", err)
	}
	if len(weeks) != 1 || weeks[0] != "2026-10-05" {
		t.Fatalf("default weeks = %v, want the UTC week of 2026-10-05", weeks)
	}

	if _, err := d.TestSQL().ExecContext(ctx,
		`UPDATE teams SET timezone = 'Europe/Moscow', week_start = 'sunday' WHERE id = ?`, teamID); err != nil {
		t.Fatal(err)
	}
	weeks, err = d.ListUnapprovedTimeWeeks(ctx, teamID, 0, from, to)
	if err != nil {
		t.Fatalf("list in the workspace zone: %v", err)
	}
	if len(weeks) != 1 || weeks[0] != "2026-10-11" {
		t.Fatalf("workspace weeks = %v, want the Moscow Sunday week of 2026-10-11", weeks)
	}
}
