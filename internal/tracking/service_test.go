package tracking

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

type activityLookupStub struct {
	ActivityStore
	query     appmodel.ActivityLookupQuery
	nameQuery appmodel.ActivityNameQuery
}

type sessionLookupStub struct {
	SessionQueryStore
	query appmodel.SessionLookupQuery
}

type sessionQueryStub struct {
	SessionQueryStore
	closed appmodel.ClosedSessionsQuery
	page   appmodel.SessionHistoryPageQuery
}

type timesheetQueryStub struct {
	TimesheetStore
	query  appmodel.TimesheetRequest
	called bool
}

func (stub *timesheetQueryStub) ListTimesheet(_ context.Context, query appmodel.TimesheetRequest) (model.TimesheetWeek, error) {
	stub.query, stub.called = query, true
	return model.TimesheetWeek{}, nil
}

func (stub *sessionQueryStub) ListClosedSessions(_ context.Context, query appmodel.ClosedSessionsQuery) ([]model.ActiveSession, error) {
	stub.closed = query
	return nil, nil
}

func (stub *sessionQueryStub) ListSessionsPage(_ context.Context, query appmodel.SessionHistoryPageQuery) ([]model.ActiveSession, bool, error) {
	stub.page = query
	return nil, false, nil
}

func (stub *sessionLookupStub) GetSession(_ context.Context, query appmodel.SessionLookupQuery) (model.Session, error) {
	stub.query = query
	return model.Session{ID: query.SessionID, TeamID: query.TeamID}, nil
}

func (stub *activityLookupStub) GetActivity(_ context.Context, query appmodel.ActivityLookupQuery) (model.Activity, error) {
	stub.query = query
	return model.Activity{ID: query.ActivityID, TeamID: query.TeamID}, nil
}

func (stub *activityLookupStub) FindActivityByName(_ context.Context, query appmodel.ActivityNameQuery) (model.Activity, error) {
	stub.nameQuery = query
	return model.Activity{TeamID: query.TeamID, Name: query.Name}, nil
}

func TestUnscopedWorkspaceIsRejected(t *testing.T) {
	service := &Service{}
	if _, err := service.ActiveSessions(context.Background(), 0); !errors.Is(err, ErrInvalidStart) {
		t.Fatalf("ActiveSessions with no workspace error = %v, want %v", err, ErrInvalidStart)
	}
	if _, err := service.ResolveActivityForMember(context.Background(), appmodel.ActivityResolveRequest{Name: "writing"}); !errors.Is(err, ErrInvalidStart) {
		t.Fatalf("ResolveActivity with no workspace error = %v, want %v", err, ErrInvalidStart)
	}
}

func TestActivityLookupKeepsWorkspaceAndActivityTogether(t *testing.T) {
	store := &activityLookupStub{}
	service := &Service{activities: store}
	got, err := service.Activity(context.Background(), 4, 9)
	if err != nil {
		t.Fatal(err)
	}
	want := appmodel.ActivityLookupQuery{TeamID: 4, ActivityID: 9}
	if got.TeamID != want.TeamID || got.ID != want.ActivityID || store.query != want {
		t.Fatalf("activity lookup = %+v, query %+v; want %+v", got, store.query, want)
	}
}

func TestActivityNameLookupKeepsWorkspaceAndNameTogether(t *testing.T) {
	store := &activityLookupStub{}
	service := &Service{activities: store}
	query := appmodel.ActivityNameQuery{TeamID: 4, Name: "  Writing  "}
	got, err := service.FindActivity(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	want := appmodel.ActivityNameQuery{TeamID: 4, Name: "Writing"}
	if got.TeamID != want.TeamID || got.Name != want.Name || store.nameQuery != want {
		t.Fatalf("activity name lookup = %+v, query %+v; want %+v", got, store.nameQuery, want)
	}
}

func TestSessionLookupKeepsWorkspaceAndSessionTogether(t *testing.T) {
	store := &sessionLookupStub{}
	service := &Service{queries: store}
	got, err := service.Session(context.Background(), 4, 9)
	if err != nil {
		t.Fatal(err)
	}
	want := appmodel.SessionLookupQuery{TeamID: 4, SessionID: 9}
	if got.TeamID != want.TeamID || got.ID != want.SessionID || store.query != want {
		t.Fatalf("session lookup = %+v, query %+v; want %+v", got, store.query, want)
	}
}

func TestClosedSessionQueryKeepsFiltersAndWorkspaceTogether(t *testing.T) {
	activityID, projectID := int64(8), int64(9)
	query := appmodel.ClosedSessionsQuery{
		TeamID: 4, Start: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		End: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), ActivityID: &activityID, ProjectID: &projectID,
	}
	store := &sessionQueryStub{}
	service := &Service{queries: store}
	if _, err := service.ClosedSessions(context.Background(), query); err != nil {
		t.Fatal(err)
	}
	if store.closed.TeamID != query.TeamID || store.closed.Start != query.Start || store.closed.End != query.End ||
		store.closed.ActivityID != query.ActivityID || store.closed.ProjectID != query.ProjectID {
		t.Fatalf("closed session query = %+v, want %+v", store.closed, query)
	}
}

func TestSessionHistoryPageNormalizesLimitAndCursorInQuery(t *testing.T) {
	store := &sessionQueryStub{}
	service := &Service{queries: store}
	query := appmodel.SessionHistoryPageQuery{
		TeamID: 4, From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		After: &model.SessionCursor{Start: "2026-01-01T01:00:00+01:00", ID: 11}, Limit: 700,
	}
	if _, err := service.SessionHistoryPage(context.Background(), query); err != nil {
		t.Fatal(err)
	}
	if store.page.Limit != 500 || store.page.After == nil || store.page.After.Start != "2026-01-01T00:00:00Z" || store.page.TeamID != query.TeamID {
		t.Fatalf("session history query = %+v, want capped limit, canonical cursor and workspace", store.page)
	}
}

func TestTimesheetKeepsGridSelectionInRequest(t *testing.T) {
	request := appmodel.TimesheetRequest{
		TeamID: 4, WeekStart: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
		Now: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC), ExtraActivityIDs: []int64{8, 9},
	}
	store := &timesheetQueryStub{}
	service := &Service{timesheets: store}
	if _, err := service.Timesheet(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if !store.called || !reflect.DeepEqual(store.query, request) {
		t.Fatalf("timesheet persistence request = %+v, want %+v", store.query, request)
	}
}

func TestTimesheetRejectsInvalidExtraActivityID(t *testing.T) {
	store := &timesheetQueryStub{}
	service := &Service{timesheets: store}
	request := appmodel.TimesheetRequest{
		TeamID: 4, WeekStart: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
		Now: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC), ExtraActivityIDs: []int64{0},
	}
	if _, err := service.Timesheet(context.Background(), request); !errors.Is(err, ErrInvalidEdit) {
		t.Fatalf("timesheet with invalid activity ID error = %v, want %v", err, ErrInvalidEdit)
	}
	if store.called {
		t.Fatal("persistence was called for invalid extra activity ID")
	}
}
