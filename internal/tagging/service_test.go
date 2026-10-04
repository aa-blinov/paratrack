package tagging

import (
	"context"
	"errors"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

type sessionActivityLookupStub struct {
	SessionActivityReader
	teamID        int64
	sessionID     int64
	sessionQuery  appmodel.SessionLookupQuery
	activityQuery appmodel.ActivityLookupQuery
}

func (stub *sessionActivityLookupStub) GetSession(_ context.Context, query appmodel.SessionLookupQuery) (model.Session, error) {
	stub.sessionQuery = query
	stub.teamID, stub.sessionID = query.TeamID, query.SessionID
	return model.Session{ID: query.SessionID, ActivityID: 12}, nil
}

func (stub *sessionActivityLookupStub) GetActivity(_ context.Context, query appmodel.ActivityLookupQuery) (model.Activity, error) {
	stub.activityQuery = query
	return model.Activity{ID: query.ActivityID, TeamID: query.TeamID}, nil
}

func TestMutationsRejectUnscopedWorkspace(t *testing.T) {
	service := &Service{}
	if _, err := service.CreateForMember(context.Background(), appmodel.TagCreateRequest{TeamID: 0, CallerID: 1, Name: "deep-work"}); !errors.Is(err, ErrInvalidTeam) {
		t.Fatalf("Create with no workspace error = %v, want %v", err, ErrInvalidTeam)
	}
	if err := service.AttachForMember(context.Background(), appmodel.SessionTagRequest{TeamID: 0, CallerID: 1, SessionID: 1, Name: "deep-work"}); !errors.Is(err, ErrInvalidTeam) {
		t.Fatalf("Attach with no workspace error = %v, want %v", err, ErrInvalidTeam)
	}
	if err := service.DetachForMember(context.Background(), appmodel.SessionTagRequest{TeamID: 0, CallerID: 1, SessionID: 1, Name: "deep-work"}); !errors.Is(err, ErrInvalidTeam) {
		t.Fatalf("Detach with no workspace error = %v, want %v", err, ErrInvalidTeam)
	}
}

func TestSessionActivityKeepsWorkspaceOnActivityLookup(t *testing.T) {
	reader := &sessionActivityLookupStub{}
	service := &Service{sessionActivities: reader}
	session, activity, err := service.SessionActivity(context.Background(), 4, 7)
	if err != nil {
		t.Fatal(err)
	}
	wantQuery := appmodel.ActivityLookupQuery{TeamID: 4, ActivityID: 12}
	wantSessionQuery := appmodel.SessionLookupQuery{TeamID: 4, SessionID: 7}
	if session.ID != 7 || activity.ID != 12 || activity.TeamID != 4 || reader.teamID != 4 || reader.sessionID != 7 || reader.sessionQuery != wantSessionQuery || reader.activityQuery != wantQuery {
		t.Fatalf("session/activity = %+v/%+v, lookup = team %d session %d query %+v", session, activity, reader.teamID, reader.sessionID, reader.activityQuery)
	}
}
