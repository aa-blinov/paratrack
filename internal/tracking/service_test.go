package tracking

import (
	"context"
	"errors"
	"testing"

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
