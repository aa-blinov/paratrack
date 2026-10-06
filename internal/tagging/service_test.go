package tagging

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

type tagListStub struct {
	TagStore
	listQuery  appmodel.TagListQuery
	countQuery appmodel.TagListQuery
}

func (stub *tagListStub) ListTags(_ context.Context, query appmodel.TagListQuery) ([]model.Tag, error) {
	stub.listQuery = query
	return nil, nil
}

func (stub *tagListStub) ListAllTagsWithCounts(_ context.Context, query appmodel.TagListQuery) ([]appmodel.TagWithCount, error) {
	stub.countQuery = query
	return nil, nil
}

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
	if _, err := service.Rename(context.Background(), appmodel.TagRenameRequest{TeamID: 0, CallerID: 1, TagID: 1, Name: "deep-work"}); !errors.Is(err, ErrInvalidTeam) {
		t.Fatalf("Rename with no workspace error = %v, want %v", err, ErrInvalidTeam)
	}
}

type tagRenameStub struct {
	TagStore
	request appmodel.TagRenameRequest
	err     error
}

func (stub *tagRenameStub) RenameTagForManager(_ context.Context, request appmodel.TagRenameRequest) (model.Tag, error) {
	stub.request = request
	if stub.err != nil {
		return model.Tag{}, stub.err
	}
	return model.Tag{ID: request.TagID, Name: request.Name, TeamID: request.TeamID}, nil
}

func TestRenameNormalizesNameBeforePersistence(t *testing.T) {
	store := &tagRenameStub{}
	service := &Service{tags: store}
	tag, err := service.Rename(context.Background(), appmodel.TagRenameRequest{TeamID: 4, CallerID: 7, TagID: 9, Name: "  Deep-Work "})
	if err != nil {
		t.Fatal(err)
	}
	if tag.Name != "deep-work" || store.request.Name != "deep-work" {
		t.Fatalf("rename reached persistence as %q, want normalized %q", store.request.Name, "deep-work")
	}
	if _, err := service.Rename(context.Background(), appmodel.TagRenameRequest{TeamID: 4, CallerID: 7, TagID: 9, Name: "  "}); !errors.Is(err, ErrInvalidTag) {
		t.Fatalf("rename to a blank name error = %v, want %v", err, ErrInvalidTag)
	}
	if _, err := service.Rename(context.Background(), appmodel.TagRenameRequest{TeamID: 4, CallerID: 7, TagID: 0, Name: "deep-work"}); !errors.Is(err, ErrInvalidTagID) {
		t.Fatalf("rename without a tag id error = %v, want %v", err, ErrInvalidTagID)
	}
}

func TestRenameReportsTakenName(t *testing.T) {
	store := &tagRenameStub{err: ErrTagNameTaken}
	service := &Service{tags: store}
	if _, err := service.Rename(context.Background(), appmodel.TagRenameRequest{TeamID: 4, CallerID: 7, TagID: 9, Name: "meetings"}); !errors.Is(err, ErrTagNameTaken) {
		t.Fatalf("taken rename name error = %v, want %v", err, ErrTagNameTaken)
	}
}

func TestTagCatalogQueriesKeepWorkspaceScope(t *testing.T) {
	store := &tagListStub{}
	service := &Service{tags: store}
	want := appmodel.TagListQuery{TeamID: 9}
	if _, err := service.List(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListWithCounts(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(store.listQuery, want) || !reflect.DeepEqual(store.countQuery, want) {
		t.Fatalf("catalog queries = %+v and %+v, want %+v", store.listQuery, store.countQuery, want)
	}
}

func TestSessionActivityKeepsWorkspaceOnActivityLookup(t *testing.T) {
	reader := &sessionActivityLookupStub{}
	service := &Service{sessionActivities: reader}
	session, activity, err := service.SessionActivity(context.Background(), appmodel.SessionLookupQuery{TeamID: 4, SessionID: 7})
	if err != nil {
		t.Fatal(err)
	}
	wantQuery := appmodel.ActivityLookupQuery{TeamID: 4, ActivityID: 12}
	wantSessionQuery := appmodel.SessionLookupQuery{TeamID: 4, SessionID: 7}
	if session.ID != 7 || activity.ID != 12 || activity.TeamID != 4 || reader.teamID != 4 || reader.sessionID != 7 || reader.sessionQuery != wantSessionQuery || reader.activityQuery != wantQuery {
		t.Fatalf("session/activity = %+v/%+v, lookup = team %d session %d query %+v", session, activity, reader.teamID, reader.sessionID, reader.activityQuery)
	}
}
