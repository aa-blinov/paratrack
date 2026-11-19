package invoicing

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// recurringStoreStub serves one scheduled workspace and keeps the letter claims
// in memory: the claim answers true exactly once per workspace and month.
type recurringStoreStub struct {
	team     RecurringWorkspace
	claims   map[string]bool
	claimErr error
}

func (s *recurringStoreStub) RecurringWorkspaces(context.Context) ([]RecurringWorkspace, error) {
	return []RecurringWorkspace{s.team}, nil
}

func (s *recurringStoreStub) ClaimRecurringInvoiceLetter(_ context.Context, request appmodel.RecurringLetterClaimRequest) (bool, error) {
	if s.claimErr != nil {
		return false, s.claimErr
	}
	key := fmt.Sprintf("%d|%s", request.TeamID, request.Month)
	if s.claims[key] {
		return false, nil
	}
	s.claims[key] = true
	return true, nil
}

type readyLetter struct {
	teamID int64
	month  string
}

type recurringNotifierStub struct {
	ready []readyLetter
}

func (n *recurringNotifierStub) ReadyToIssue(_ context.Context, teamID int64, month string, _ []DraftSummary, _ []SkippedDraft) {
	n.ready = append(n.ready, readyLetter{teamID: teamID, month: month})
}

// The schedule creates a draft per billable project for the previous calendar
// month in the workspace's zone, skips projects with nothing to bill, and the
// "ready to issue" letter goes out once for the month however many ticks pass.
func TestRecurringPassCreatesPreviousMonthDrafts(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	rate := 5000
	store := &recurringStoreStub{
		team:   RecurringWorkspace{TeamID: 1, OwnerID: 9, Zone: berlin},
		claims: map[string]bool{},
	}
	projects := projectBillingStub{projects: []model.Project{
		{ID: 7, Name: "Сайт", Billable: true, BillableRateCents: &rate},
		{ID: 8, Name: "Внутрянка", Billable: false, BillableRateCents: &rate}, // never billable
		{ID: 9, Name: "Портал", Billable: true},                               // billable, no rate
		{ID: 10, Name: "Праздник", Billable: true, BillableRateCents: &rate, Archived: true},
	}}
	writer := &draftWriterStub{
		invoice: model.Invoice{Number: "INV-2026-012", Currency: "RUB"},
		lines:   []model.InvoiceLine{{AmountCents: 100_000, Currency: "RUB"}},
	}
	notifier := &recurringNotifierStub{}
	service, err := NewService(Dependencies{
		Reader: &draftReaderStub{}, Projects: projects, Writer: writer,
		Audit: noopAuditRecorder{}, Logger: noopLogger{},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The pass runs on the 1st of December, Berlin morning: November is the
	// month it bills, down to its last second.
	now := time.Date(2026, 12, 1, 9, 15, 0, 0, berlin)
	if err := service.startRecurring(store, notifier, func() time.Time { return now }); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := service.RecurringPass(ctx); err != nil {
		t.Fatalf("pass: %v", err)
	}
	if len(writer.drafted) != 1 {
		t.Fatalf("pass drafted %d invoices, want one (the only billable project)", len(writer.drafted))
	}
	draft := writer.drafted[0]
	if draft.TeamID != 1 || draft.CallerID != 9 {
		t.Fatalf("draft = %+v, want team 1 acting from its owner", draft)
	}
	if draft.Options.ProjectID != 7 {
		t.Fatalf("draft project = %d, want 7", draft.Options.ProjectID)
	}
	start, end := draft.Start, draft.End
	if start.Format("2006-01-02 15:04") != "2026-11-01 00:00" {
		t.Fatalf("draft start = %s, want the 1st of November in Berlin", start)
	}
	if end.Format("2006-01-02 15:04") != "2026-11-30 23:59" {
		t.Fatalf("draft end = %s, want the last minute of November", end)
	}
	if len(notifier.ready) != 1 || notifier.ready[0].month != "2026-11" || notifier.ready[0].teamID != 1 {
		t.Fatalf("letter = %+v, want one for 2026-11 to team 1", notifier.ready)
	}

	// A second tick claims nothing: the month's letter is once, and the draft
	// the letter already named repeats itself only if the server lost it.
	if err := service.RecurringPass(ctx); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if len(notifier.ready) != 1 {
		t.Fatalf("the second pass repeated the letter: %+v", notifier.ready)
	}
}

// A project whose week nobody approved holds its draft back: the schedule
// confirms nothing in silence (D-3). The letter still goes out for the drafts
// that did become ready.
func TestRecurringPassHoldsUnapprovedWeeksBack(t *testing.T) {
	rate := 5000
	store := &recurringStoreStub{
		team:   RecurringWorkspace{TeamID: 1, OwnerID: 9, Zone: time.UTC},
		claims: map[string]bool{},
	}
	projects := projectBillingStub{projects: []model.Project{
		{ID: 7, Name: "Сайт", Billable: true, BillableRateCents: &rate},
		{ID: 12, Name: "Аудит", Billable: true, BillableRateCents: &rate},
	}}
	reader := &unapprovedReaderStub{days: map[int64][]string{12: {"2026-11-17", "2026-11-19"}}}
	writer := &draftWriterStub{
		invoice: model.Invoice{Number: "INV-2026-005", Currency: "RUB"},
		lines:   []model.InvoiceLine{{AmountCents: 40_000, Currency: "RUB"}},
	}
	notifier := &recurringNotifierStub{}
	service, err := NewService(Dependencies{
		Reader: reader, Projects: projects, Writer: writer,
		Audit: noopAuditRecorder{}, Logger: noopLogger{},
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 12, 1, 0, 30, 0, 0, time.UTC)
	if err := service.startRecurring(store, notifier, func() time.Time { return now }); err != nil {
		t.Fatal(err)
	}
	if err := service.RecurringPass(context.Background()); err != nil {
		t.Fatalf("pass: %v", err)
	}
	if len(writer.drafted) != 1 || writer.drafted[0].Options.ProjectID != 7 {
		t.Fatalf("drafts = %+v, want only the project without unapproved weeks", writer.drafted)
	}
	if len(notifier.ready) != 1 {
		t.Fatalf("ready letters = %+v, want one — the drafts that did appear", notifier.ready)
	}
}

type unapprovedReaderStub struct {
	draftReaderStub
	// days keys project id → the days with billable time outside approved weeks.
	days map[int64][]string
}

func (s *unapprovedReaderStub) ListUnapprovedTimeDays(_ context.Context, _, projectID int64, _, _ time.Time) ([]string, error) {
	return s.days[projectID], nil
}
