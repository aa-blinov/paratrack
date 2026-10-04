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
	activityQuery appmodel.ActivityLookupQuery
}

func (stub *sessionActivityLookupStub) GetSession(_ context.Context, teamID, sessionID int64) (model.Session, error) {
	stub.teamID, stub.sessionID = teamID, sessionID
	return model.Session{ID: sessionID, ActivityID: 12}, nil
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
	if session.ID != 7 || activity.ID != 12 || activity.TeamID != 4 || reader.teamID != 4 || reader.sessionID != 7 || reader.activityQuery != wantQuery {
		t.Fatalf("session/activity = %+v/%+v, lookup = team %d session %d query %+v", session, activity, reader.teamID, reader.sessionID, reader.activityQuery)
	}
}
