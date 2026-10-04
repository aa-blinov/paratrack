package sessiondecorations

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

type tagReaderStub struct {
	ids []int64
	by  map[int64][]model.Tag
	err error
}

func (stub *tagReaderStub) TagsForSessions(_ context.Context, _ int64, ids []int64) (map[int64][]model.Tag, error) {
	stub.ids = append([]int64(nil), ids...)
	return stub.by, stub.err
}

type projectReaderStub struct {
	teamID int64
	ids    []int64
	by     map[int64]model.ProjectSummary
	err    error
}

type sessionReaderStub struct {
	sessionID int64
	teamID    int64
	session   model.Session
	activity  model.Activity
	err       error
}

func (stub *sessionReaderStub) SessionActivity(_ context.Context, teamID, sessionID int64) (model.Session, model.Activity, error) {
	stub.teamID, stub.sessionID = teamID, sessionID
	return stub.session, stub.activity, stub.err
}

func (stub *projectReaderStub) Summaries(_ context.Context, query appmodel.ProjectSummariesQuery) (map[int64]model.ProjectSummary, error) {
	stub.teamID = query.TeamID
	stub.ids = append([]int64(nil), query.ProjectIDs...)
	return stub.by, stub.err
}

type loggerStub struct{ messages []string }

func (stub *loggerStub) Printf(format string, args ...any) {
	stub.messages = append(stub.messages, fmt.Sprintf(format, args...))
}

func TestBuildBatchesDistinctSessionAndProjectIDs(t *testing.T) {
	tags := &tagReaderStub{by: map[int64][]model.Tag{2: {{ID: 8, Name: "urgent"}}}}
	projects := &projectReaderStub{by: map[int64]model.ProjectSummary{7: {ID: 7, Name: "Client"}}}
	builder, err := New(Dependencies{Sessions: &sessionReaderStub{}, Tags: tags, Projects: projects, Logger: &loggerStub{}})
	if err != nil {
		t.Fatal(err)
	}
	sessions := []model.ActiveSession{
		{Session: model.Session{ID: 2}, Activity: model.Activity{ProjectID: 7}},
		{Session: model.Session{ID: 2}, Activity: model.Activity{ProjectID: 7}},
		{Session: model.Session{ID: 3}},
	}
	snapshot, err := builder.Build(context.Background(), appmodel.SessionDecorationRequest{
		TeamID: 4, Sessions: sessions, IncludeTags: true, IncludeProjects: true,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !reflect.DeepEqual(tags.ids, []int64{2, 3}) || !reflect.DeepEqual(projects.ids, []int64{7}) || projects.teamID != 4 {
		t.Fatalf("batch query: sessions=%v projects=%v team=%d", tags.ids, projects.ids, projects.teamID)
	}
	if snapshot.TagsBySession[2][0].Name != "urgent" || snapshot.ProjectsByID[7].Name != "Client" {
		t.Fatalf("decoration snapshot = %+v", snapshot)
	}
}

func TestBuildKeepsSessionRowsWhenOptionalDecorationsFail(t *testing.T) {
	logger := &loggerStub{}
	builder, err := New(Dependencies{
		Sessions: &sessionReaderStub{},
		Tags:     &tagReaderStub{err: errors.New("tag store unavailable")},
		Projects: &projectReaderStub{err: errors.New("project store unavailable")}, Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := builder.Build(context.Background(), appmodel.SessionDecorationRequest{
		TeamID: 4, Sessions: []model.ActiveSession{{Session: model.Session{ID: 1}, Activity: model.Activity{ProjectID: 9}}},
		IncludeTags: true, IncludeProjects: true,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(snapshot.TagsBySession) != 0 || len(snapshot.ProjectsByID) != 0 || len(logger.messages) != 2 {
		t.Fatalf("failed decorations should be empty and logged: snapshot=%+v logs=%v", snapshot, logger.messages)
	}
}

func TestBuildRowLoadsSessionAndDecoratesItInOneWorkflow(t *testing.T) {
	sessions := &sessionReaderStub{session: model.Session{ID: 12}, activity: model.Activity{ID: 4, ProjectID: 9}}
	tags := &tagReaderStub{by: map[int64][]model.Tag{12: {{ID: 3, Name: "priority"}}}}
	projects := &projectReaderStub{by: map[int64]model.ProjectSummary{9: {ID: 9, Name: "Client"}}}
	builder, err := New(Dependencies{Sessions: sessions, Tags: tags, Projects: projects, Logger: &loggerStub{}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := builder.BuildRow(context.Background(), 5, 12)
	if err != nil {
		t.Fatalf("BuildRow: %v", err)
	}
	if sessions.teamID != 5 || sessions.sessionID != 12 || snapshot.Session.Session.ID != 12 ||
		snapshot.Decorations.TagsBySession[12][0].Name != "priority" || snapshot.Decorations.ProjectsByID[9].Name != "Client" {
		t.Fatalf("session row snapshot = %+v", snapshot)
	}
}
