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
	query appmodel.ActivityLookupQuery
}

func (stub *activityLookupStub) GetActivity(_ context.Context, query appmodel.ActivityLookupQuery) (model.Activity, error) {
	stub.query = query
	return model.Activity{ID: query.ActivityID, TeamID: query.TeamID}, nil
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
