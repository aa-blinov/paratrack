package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// auditTrailFixture records events for two workspaces on a known clock, so the
// filters can be checked against exact times instead of "now".
func auditTrailFixture(t *testing.T, d *DB) time.Time {
	t.Helper()
	day := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	clock := day
	d.now = func() time.Time { return clock }
	record := func(teamID, userID int64, action, target string) {
		t.Helper()
		if err := d.Audit(t.Context(), model.AuditRecord{
			TeamID: teamID, UserID: userID, Action: action, Target: target, IP: "127.0.0.1",
		}); err != nil {
			t.Fatalf("record audit event %s: %v", action, err)
		}
		clock = clock.Add(2 * time.Hour)
	}
	record(7, 1, "auth.login", "sign-in")
	record(7, 2, "invoice.paid", "INV-1")
	record(7, 1, "invoice.paid", "INV-2")
	record(7, 1, "session.start", "9")
	record(8, 1, "auth.login", "other-workspace")
	return day
}

func auditTargets(t *testing.T, d *DB, query appmodel.AuditListQuery) []string {
	t.Helper()
	entries, err := d.ListAudit(t.Context(), query)
	if err != nil {
		t.Fatalf("ListAudit(%+v): %v", query, err)
	}
	targets := make([]string, 0, len(entries))
	for _, entry := range entries {
		targets = append(targets, entry.Target)
	}
	return targets
}

func assertTargets(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("targets = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("targets = %v, want %v", got, want)
		}
	}
}

func TestListAuditFiltersNarrowTheWorkspaceTrail(t *testing.T) {
	d := openTestDB(t)
	day := auditTrailFixture(t, d)
	all := []string{"9", "INV-2", "INV-1", "sign-in"}

	cases := []struct {
		name  string
		query appmodel.AuditListQuery
		want  []string
	}{
		{"no filter keeps the whole trail", appmodel.AuditListQuery{TeamID: 7}, all},
		{"action keeps one event type", appmodel.AuditListQuery{TeamID: 7, Action: "invoice.paid"}, []string{"INV-2", "INV-1"}},
		{"actor keeps one person", appmodel.AuditListQuery{TeamID: 7, UserID: 2}, []string{"INV-1"}},
		{"period keeps one day of events", appmodel.AuditListQuery{
			TeamID: 7, From: day.Add(3 * time.Hour), To: day.Add(5 * time.Hour),
		}, []string{"INV-2"}},
		{"action and actor combine", appmodel.AuditListQuery{TeamID: 7, Action: "invoice.paid", UserID: 1}, []string{"INV-2"}},
		{"all three filters combine", appmodel.AuditListQuery{
			TeamID: 7, From: day, To: day.Add(48 * time.Hour), Action: "invoice.paid", UserID: 1,
		}, []string{"INV-2"}},
		{"a filter nothing matches is empty", appmodel.AuditListQuery{TeamID: 7, Action: "auth.login", UserID: 2}, []string{}},
		{"a period before the trail is empty", appmodel.AuditListQuery{
			TeamID: 7, From: day.Add(-48 * time.Hour), To: day.Add(-24 * time.Hour),
		}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.query.Limit = 100
			assertTargets(t, auditTargets(t, d, tc.query), tc.want)
		})
	}
}

func TestListAuditKeepsOtherWorkspacesOutOfTheTrail(t *testing.T) {
	d := openTestDB(t)
	auditTrailFixture(t, d)
	assertTargets(t, auditTargets(t, d, appmodel.AuditListQuery{TeamID: 7, Limit: 100}), []string{"9", "INV-2", "INV-1", "sign-in"})
	assertTargets(t, auditTargets(t, d, appmodel.AuditListQuery{TeamID: 8, Limit: 100}), []string{"other-workspace"})
	assertTargets(t, auditTargets(t, d, appmodel.AuditListQuery{TeamID: 8, UserID: 2, Limit: 100}), []string{})
}

func TestListAuditOffsetContinuesTheTrailWithoutRepeatingRows(t *testing.T) {
	d := openTestDB(t)
	auditTrailFixture(t, d)
	assertTargets(t, auditTargets(t, d, appmodel.AuditListQuery{TeamID: 7, Limit: 2, Offset: 2}), []string{"INV-1", "sign-in"})
	// A negative offset is a broken address, not a request to read from the end.
	assertTargets(t, auditTargets(t, d, appmodel.AuditListQuery{TeamID: 7, Limit: 100, Offset: -5}), []string{"9", "INV-2", "INV-1", "sign-in"})
}

func TestListAuditRejectsMissingTeamScope(t *testing.T) {
	d := openTestDB(t)
	if _, err := d.ListAudit(t.Context(), appmodel.AuditListQuery{Limit: 100}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ListAudit with no workspace = %v, want ErrNotFound", err)
	}
}
