package dashboard

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

type loggerStub struct{ messages []string }

func (l *loggerStub) Printf(format string, args ...any) {
	l.messages = append(l.messages, fmt.Sprintf(format, args...))
}

type goalsStub struct{}

func (goalsStub) Activities(context.Context, int64) ([]model.Activity, error) { return nil, nil }
func (goalsStub) Progress(context.Context, int64, time.Time) ([]model.GoalProgress, error) {
	return nil, errors.New("optional widget unavailable")
}

type trackingStub struct {
	closedRanges [][2]time.Time
	checkedAny   int
	withSessions bool
	anySession   bool
}

func (s *trackingStub) ActiveSessions(context.Context, int64) ([]model.ActiveSession, error) {
	if !s.withSessions {
		return nil, nil
	}
	return []model.ActiveSession{{Session: model.Session{ID: 3}, Activity: model.Activity{ProjectID: 9}}}, nil
}

func (s *trackingStub) ClosedSessions(_ context.Context, _ int64, from, to time.Time, _ *int64) ([]model.ActiveSession, error) {
	s.closedRanges = append(s.closedRanges, [2]time.Time{from, to})
	if !s.withSessions {
		return nil, nil
	}
	return []model.ActiveSession{{Session: model.Session{ID: 3}, Activity: model.Activity{ProjectID: 9}}}, nil
}

func (s *trackingStub) HasAnySession(context.Context, int64) (bool, error) {
	s.checkedAny++
	return s.anySession, nil
}

type projectsStub struct{ summaryIDs []int64 }

func (*projectsStub) List(context.Context, int64, bool) ([]model.Project, error) {
	return []model.Project{{ID: 1}}, nil
}

func (s *projectsStub) Summaries(_ context.Context, _ int64, ids []int64) (map[int64]model.ProjectSummary, error) {
	s.summaryIDs = append(s.summaryIDs, ids...)
	return map[int64]model.ProjectSummary{}, nil
}

type tagsStub struct {
	calls int
	ids   []int64
}

func (s *tagsStub) TagsForSessions(_ context.Context, _ int64, ids []int64) (map[int64][]model.Tag, error) {
	s.calls++
	s.ids = append(s.ids, ids...)
	return map[int64][]model.Tag{}, nil
}

type invoicesStub struct{ calls int }

func (s *invoicesStub) UnbilledProjectTime(context.Context, int64, int64) ([]model.UnbilledProject, error) {
	s.calls++
	return nil, nil
}

func TestBuildUsesDashboardWindowsAndOptionalBilling(t *testing.T) {
	tracking := &trackingStub{withSessions: true}
	projects := &projectsStub{}
	tags := &tagsStub{}
	invoices := &invoicesStub{}
	logger := &loggerStub{}
	builder, err := NewBuilder(Dependencies{
		Goals: goalsStub{}, Tracking: tracking, Projects: projects, Tags: tags, Invoices: invoices,
		Logger: logger,
	})
	if err != nil {
		t.Fatalf("construct dashboard builder: %v", err)
	}
	now := time.Date(2026, time.October, 2, 15, 30, 0, 0, time.UTC)
	snapshot, err := builder.Build(context.Background(), Query{TeamID: 7, Now: now, IncludeBilling: true})
	if err != nil {
		t.Fatalf("build dashboard: %v", err)
	}
	if !snapshot.TodayStart.Equal(time.Date(2026, time.October, 2, 0, 0, 0, 0, time.UTC)) || !snapshot.TodayEnd.Equal(now) {
		t.Fatalf("unexpected today view range: %s..%s", snapshot.TodayStart, snapshot.TodayEnd)
	}
	if len(tracking.closedRanges) != 2 {
		t.Fatalf("got %d closed-session ranges, want 2", len(tracking.closedRanges))
	}
	wantTodayEnd := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	if !tracking.closedRanges[0][0].Equal(snapshot.TodayStart) || !tracking.closedRanges[0][1].Equal(wantTodayEnd) {
		t.Fatalf("unexpected today query range: %s..%s", tracking.closedRanges[0][0], tracking.closedRanges[0][1])
	}
	if !tracking.closedRanges[1][0].Equal(now.AddDate(0, 0, -7)) || !tracking.closedRanges[1][1].Equal(now) {
		t.Fatalf("unexpected recent query range: %s..%s", tracking.closedRanges[1][0], tracking.closedRanges[1][1])
	}
	if !snapshot.HasSession || tracking.checkedAny != 0 {
		t.Fatalf("existing-session snapshot is incorrect: hasSession=%v fallback checks=%d", snapshot.HasSession, tracking.checkedAny)
	}
	if tags.calls != 1 || len(tags.ids) != 1 || tags.ids[0] != 3 {
		t.Fatalf("session tags were not loaded once for distinct session IDs: calls=%d ids=%v", tags.calls, tags.ids)
	}
	if len(projects.summaryIDs) != 1 || projects.summaryIDs[0] != 9 {
		t.Fatalf("project summaries were not loaded once for distinct project IDs: %v", projects.summaryIDs)
	}
	if invoices.calls != 1 {
		t.Fatalf("unbilled-time lookup called %d times, want 1", invoices.calls)
	}
	if len(snapshot.Goals) != 0 {
		t.Fatalf("failed optional goal widget returned %d rows", len(snapshot.Goals))
	}
	if len(logger.messages) != 1 || !strings.Contains(logger.messages[0], "load goal progress for team 7") {
		t.Fatalf("optional widget failure logs = %v, want one diagnostic", logger.messages)
	}
}

func TestBuildUsesCalendarDayAcrossDST(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	tracking := &trackingStub{}
	builder, err := NewBuilder(Dependencies{
		Goals: goalsStub{}, Tracking: tracking, Projects: &projectsStub{}, Tags: &tagsStub{}, Invoices: &invoicesStub{},
		Logger: &loggerStub{},
	})
	if err != nil {
		t.Fatalf("construct dashboard builder: %v", err)
	}
	now := time.Date(2026, time.March, 8, 12, 0, 0, 0, location)
	_, err = builder.Build(context.Background(), Query{TeamID: 7, Now: now})
	if err != nil {
		t.Fatalf("build dashboard: %v", err)
	}
	if len(tracking.closedRanges) == 0 {
		t.Fatal("dashboard did not query closed sessions")
	}
	today := tracking.closedRanges[0]
	if today[1].Sub(today[0]) != 23*time.Hour || today[1].Hour() != 0 {
		t.Fatalf("today query range = %s..%s, want a 23-hour local calendar day", today[0], today[1])
	}
}

func TestBuildUsesFirstRunFallbackWhenRecentSessionsAreEmpty(t *testing.T) {
	tracking := &trackingStub{anySession: true}
	invoices := &invoicesStub{}
	builder, err := NewBuilder(Dependencies{
		Goals: goalsStub{}, Tracking: tracking, Projects: &projectsStub{}, Tags: &tagsStub{}, Invoices: invoices,
		Logger: &loggerStub{},
	})
	if err != nil {
		t.Fatalf("construct dashboard builder: %v", err)
	}
	snapshot, err := builder.Build(context.Background(), Query{
		TeamID: 7, Now: time.Date(2026, time.October, 2, 15, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("build dashboard: %v", err)
	}
	if !snapshot.HasSession || tracking.checkedAny != 1 {
		t.Fatalf("first-run fallback was not applied: hasSession=%v checks=%d", snapshot.HasSession, tracking.checkedAny)
	}
	if invoices.calls != 0 {
		t.Fatalf("billing query ran while billing widget was disabled: %d calls", invoices.calls)
	}
}

func TestBuildActiveListCoordinatesRowsAndFirstRunState(t *testing.T) {
	tracking := &trackingStub{withSessions: true}
	projects := &projectsStub{}
	tags := &tagsStub{}
	logger := &loggerStub{}
	builder, err := NewBuilder(Dependencies{
		Goals: goalsStub{}, Tracking: tracking, Projects: projects, Tags: tags,
		Invoices: &invoicesStub{}, Logger: logger,
	})
	if err != nil {
		t.Fatalf("construct dashboard builder: %v", err)
	}
	snapshot, err := builder.BuildActiveList(context.Background(), 7)
	if err != nil {
		t.Fatalf("build active list: %v", err)
	}
	if len(snapshot.ActiveSessions) != 1 || len(snapshot.Projects) != 1 || snapshot.FirstRun {
		t.Fatalf("active list snapshot = %+v", snapshot)
	}
	if tags.calls != 1 || len(tags.ids) != 1 || tags.ids[0] != 3 {
		t.Fatalf("session tags were not loaded once: calls=%d ids=%v", tags.calls, tags.ids)
	}
	if len(projects.summaryIDs) != 1 || projects.summaryIDs[0] != 9 {
		t.Fatalf("project summaries were not loaded once: %v", projects.summaryIDs)
	}
	if tracking.checkedAny != 0 {
		t.Fatalf("active sessions triggered first-run lookup %d times", tracking.checkedAny)
	}
}

func TestBuildActiveListUsesFirstRunFallback(t *testing.T) {
	tracking := &trackingStub{}
	builder, err := NewBuilder(Dependencies{
		Goals: goalsStub{}, Tracking: tracking, Projects: &projectsStub{}, Tags: &tagsStub{},
		Invoices: &invoicesStub{}, Logger: &loggerStub{},
	})
	if err != nil {
		t.Fatalf("construct dashboard builder: %v", err)
	}
	snapshot, err := builder.BuildActiveList(context.Background(), 7)
	if err != nil {
		t.Fatalf("build active list: %v", err)
	}
	if !snapshot.FirstRun || tracking.checkedAny != 1 {
		t.Fatalf("first-run state = %v after %d history checks, want true after one", snapshot.FirstRun, tracking.checkedAny)
	}
}
