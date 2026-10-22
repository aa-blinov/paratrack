package invoicing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// unapprovedDaysReader answers the unapproved-days question with planted days
// and remembers which project it was asked about.
type unapprovedDaysReader struct {
	*draftReaderStub
	days    []string
	project int64
}

func (s *unapprovedDaysReader) ListUnapprovedTimeDays(_ context.Context, _, projectID int64, _, _ time.Time) ([]string, error) {
	s.project = projectID
	return s.days, nil
}

// A week is named by its Monday, so a Sunday belongs to the week that began six
// days earlier (FIN-12).
func TestUnapprovedWeeksNameEachWeekByItsMonday(t *testing.T) {
	reader := &unapprovedDaysReader{draftReaderStub: &draftReaderStub{}, days: []string{"2026-10-12", "2026-10-14", "2026-10-18", "2026-10-19"}}
	service, err := NewService(testDependencies(reader, &draftWriterStub{}))
	if err != nil {
		t.Fatal(err)
	}
	weeks, err := service.UnapprovedWeeks(t.Context(), 7, 3, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, monday := range weeks {
		got = append(got, monday.Format("2006-01-02"))
	}
	if want := []string{"2026-10-12", "2026-10-19"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("weeks = %v, want %v", got, want)
	}
	if reader.project != 3 {
		t.Fatalf("asked about project %d, want 3", reader.project)
	}
	if _, err := service.UnapprovedWeeks(t.Context(), 0, 3, time.Time{}, time.Time{}); !errors.Is(err, ErrInvalidTeam) {
		t.Fatalf("no workspace: err = %v, want ErrInvalidTeam", err)
	}
}

// The draft waits for the manager: without the confirmation it names the weeks
// and writes nothing; with it the draft goes on as usual (D-3).
func TestDraftWaitsForTheManagerAboutUnapprovedWeeks(t *testing.T) {
	reader := &unapprovedDaysReader{draftReaderStub: &draftReaderStub{}, days: []string{"2026-10-12"}}
	service, err := NewService(testDependencies(reader, &draftWriterStub{}))
	if err != nil {
		t.Fatal(err)
	}
	request := appmodel.InvoiceDraftRequest{
		TeamID: 7, CallerID: 3, Client: "Acme",
		Start: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC),
	}

	_, err = service.CreateDraftWithOverlapCheck(t.Context(), request)
	var unapproved *model.UnapprovedWeeksError
	if !errors.As(err, &unapproved) || len(unapproved.Weeks) != 1 {
		t.Fatalf("unconfirmed draft: err = %v, want the unapproved week named", err)
	}
	if !errors.Is(err, model.ErrWeekNotApproved) {
		t.Fatalf("the refusal must match ErrWeekNotApproved, got %v", err)
	}

	request.ConfirmUnapproved = true
	_, err = service.CreateDraftWithOverlapCheck(t.Context(), request)
	if errors.As(err, &unapproved) {
		t.Fatalf("a confirmed draft was still refused for unapproved weeks: %v", err)
	}
}
