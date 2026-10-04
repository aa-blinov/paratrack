package integrationtracking

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

type taskReaderStub struct {
	calls          int
	teamID, taskID int64
	query          appmodel.ExternalTaskLookupQuery
	task           model.ExternalTask
	err            error
}

func (stub *taskReaderStub) Task(_ context.Context, query appmodel.ExternalTaskLookupQuery) (model.ExternalTask, error) {
	stub.calls++
	stub.query = query
	stub.teamID, stub.taskID = query.TeamID, query.TaskID
	return stub.task, stub.err
}

type timerStarterStub struct {
	calls    int
	request  appmodel.TimerStartByNameRequest
	activity model.Activity
	session  model.Session
	err      error
}

func (stub *timerStarterStub) StartActivity(_ context.Context, request appmodel.TimerStartByNameRequest) (model.Activity, model.Session, error) {
	stub.calls++
	stub.request = request
	return stub.activity, stub.session, stub.err
}

func TestStartCoordinatesImportedTaskAndTimer(t *testing.T) {
	reader := &taskReaderStub{task: model.ExternalTask{ID: 31, Title: "Fix issue"}}
	timer := &timerStarterStub{activity: model.Activity{ID: 7, Name: "Fix issue"}, session: model.Session{ID: 88}}
	builder, err := New(Dependencies{Tasks: reader, Timers: timer})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	activity, session, err := builder.Start(context.Background(), appmodel.ImportedTaskStartRequest{
		TeamID: 4, CallerID: 12, TaskID: 31, ProjectID: 9, At: at, Note: "from integration",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reader.teamID != 4 || reader.taskID != 31 || reader.query != (appmodel.ExternalTaskLookupQuery{TeamID: 4, TaskID: 31}) {
		t.Fatalf("task lookup scope = team %d task %d", reader.teamID, reader.taskID)
	}
	if timer.request.TeamID != 4 || timer.request.CallerID != 12 || timer.request.ActivityName != "Fix issue" || timer.request.ProjectID != 9 || !timer.request.At.Equal(at) || timer.request.Note != "from integration" {
		t.Fatalf("timer start request = %+v", timer.request)
	}
	if activity.ID != 7 || session.ID != 88 {
		t.Fatalf("start result = activity %+v, session %+v", activity, session)
	}
}

func TestStartStopsWhenImportedTaskCannotBeLoaded(t *testing.T) {
	wantErr := errors.New("task unavailable")
	reader := &taskReaderStub{err: wantErr}
	timer := &timerStarterStub{}
	builder, err := New(Dependencies{Tasks: reader, Timers: timer})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = builder.Start(context.Background(), appmodel.ImportedTaskStartRequest{TeamID: 4, TaskID: 31})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Start error = %v, want imported task error", err)
	}
	if timer.calls != 0 {
		t.Fatalf("timer started after task lookup failure: %+v", timer.request)
	}
}

func TestStartRejectsInvalidScopeBeforeReadingTask(t *testing.T) {
	reader := &taskReaderStub{task: model.ExternalTask{Title: "Fix issue"}}
	timer := &timerStarterStub{}
	builder, err := New(Dependencies{Tasks: reader, Timers: timer})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = builder.Start(context.Background(), appmodel.ImportedTaskStartRequest{TeamID: 0, TaskID: 31})
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("Start invalid scope error = %v, want not found", err)
	}
	if reader.calls != 0 || timer.calls != 0 {
		t.Fatalf("invalid scope reached dependencies: task reads=%d timer starts=%d", reader.calls, timer.calls)
	}
}

func TestStartReturnsResolvedActivityWhenTimerStartFails(t *testing.T) {
	wantErr := errors.New("timer already active")
	timer := &timerStarterStub{activity: model.Activity{ID: 7, Name: "Fix issue"}, err: wantErr}
	builder, err := New(Dependencies{Tasks: &taskReaderStub{task: model.ExternalTask{Title: "Fix issue"}}, Timers: timer})
	if err != nil {
		t.Fatal(err)
	}
	activity, _, err := builder.Start(context.Background(), appmodel.ImportedTaskStartRequest{TeamID: 4, CallerID: 12, TaskID: 31})
	if !errors.Is(err, wantErr) || activity.ID != 7 {
		t.Fatalf("Start result = activity %+v, error %v", activity, err)
	}
}
