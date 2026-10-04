package trackingops

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

type sessionStarterStub struct {
	session         model.Session
	stopAllSessions []model.Session
	focus           appmodel.FocusResult
	deleteCalls     int
	err             error
	resolved        model.Activity
	project         appmodel.AssignActivityProjectRequest
	closed          appmodel.TimerAddRequest
	startCalls      int
}

func (s *sessionStarterStub) Start(context.Context, appmodel.TimerStartRequest) (model.Session, error) {
	s.startCalls++
	return s.session, s.err
}

func (*sessionStarterStub) Activity(_ context.Context, _ int64, activityID int64) (model.Activity, error) {
	return model.Activity{ID: activityID, Name: "Deep work"}, nil
}

func (s *sessionStarterStub) ResolveActivityForMember(_ context.Context, request appmodel.ActivityResolveRequest) (model.Activity, error) {
	s.resolved = model.Activity{ID: 7, Name: request.Name}
	return s.resolved, s.err
}

func (s *sessionStarterStub) AssignActivity(_ context.Context, request appmodel.AssignActivityProjectRequest) error {
	s.project = request
	return s.err
}

func (s *sessionStarterStub) AddClosed(_ context.Context, request appmodel.TimerAddRequest) (model.Session, error) {
	s.closed = request
	return s.session, s.err
}

func (s *sessionStarterStub) Focus(context.Context, appmodel.TimerFocusRequest) (appmodel.FocusResult, error) {
	return s.focus, s.err
}

func (s *sessionStarterStub) Stop(context.Context, appmodel.TimerStopRequest) (model.Session, error) {
	return s.session, s.err
}

func (s *sessionStarterStub) StopAll(context.Context, appmodel.TimerStopAllRequest) ([]model.Session, error) {
	return s.stopAllSessions, s.err
}

func (s *sessionStarterStub) Reopen(context.Context, appmodel.TimerReopenRequest) (model.Session, error) {
	return s.session, s.err
}

func (s *sessionStarterStub) Delete(context.Context, appmodel.SessionDeleteRequest) error {
	s.deleteCalls++
	return s.err
}

type goalProgressReaderStub struct {
	goals        []model.GoalProgress
	stoppedBatch map[int64]int
}

func (s *goalProgressReaderStub) NewlyAchievedAfterSessions(_ context.Context, _ int64, stopped map[int64]int, _ time.Time) ([]model.GoalProgress, error) {
	s.stoppedBatch = stopped
	return s.goals, nil
}

type notificationsStub struct{ stopped, achieved int }

func (s *notificationsStub) SessionStopped(context.Context, appmodel.SessionStoppedNotification) error {
	s.stopped++
	return nil
}

func (s *notificationsStub) GoalAchieved(context.Context, appmodel.GoalAchievedNotification) error {
	s.achieved++
	return nil
}

func dependencies(sessions *sessionStarterStub, audit *auditRecorderStub) Dependencies {
	return Dependencies{
		Sessions: sessions, Activities: sessions, Resolver: sessions, Projects: sessions,
		ClosedSessions: sessions, Goals: &goalProgressReaderStub{},
		Audit: audit, Notifications: &notificationsStub{},
		Logger: log.New(io.Discard, "", 0),
	}
}

func TestStartActivityResolvesAssignsAndStartsThroughOneOperation(t *testing.T) {
	sessions := &sessionStarterStub{session: model.Session{ID: 81}}
	audit := &auditRecorderStub{}
	service, err := New(dependencies(sessions, audit))
	if err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithActor(context.Background(), 12)
	activity, session, err := service.StartActivity(ctx, appmodel.TimerStartByNameRequest{
		TeamID: 4, CallerID: 12, ActivityName: "Deep work", ProjectID: 9,
		At: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Note: "note",
	})
	if err != nil {
		t.Fatal(err)
	}
	if activity.ID != 7 || activity.Name != "Deep work" || session.ID != 81 {
		t.Fatalf("start activity result = activity %+v, session %+v", activity, session)
	}
	if sessions.project.TeamID != 4 || sessions.project.CallerID != 12 || sessions.project.ActivityID != 7 || sessions.project.ProjectID != 9 {
		t.Fatalf("project assignment request = %+v", sessions.project)
	}
	if audit.calls != 1 || audit.action != "session.start" {
		t.Fatalf("start audit = %+v", audit)
	}
}

func TestAddClosedActivityResolvesAssignsAndPersistsClosedSession(t *testing.T) {
	sessions := &sessionStarterStub{session: model.Session{ID: 82}}
	audit := &auditRecorderStub{}
	service, err := New(dependencies(sessions, audit))
	if err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithActor(context.Background(), 12)
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	end := start.Add(time.Hour)
	activity, session, err := service.AddClosedActivity(ctx, appmodel.TimerAddByNameRequest{
		TeamID: 4, CallerID: 12, ActivityName: "Deep work", ProjectID: 9,
		Start: start, End: end, Note: "note",
	})
	if err != nil {
		t.Fatal(err)
	}
	if activity.ID != 7 || session.ID != 82 {
		t.Fatalf("backfill result = activity %+v, session %+v", activity, session)
	}
	if sessions.closed.TeamID != 4 || sessions.closed.ActivityID != 7 || !sessions.closed.Start.Equal(start) || !sessions.closed.End.Equal(end) || sessions.closed.Note != "note" {
		t.Fatalf("closed-session request = %+v", sessions.closed)
	}
	if sessions.project.ProjectID != 9 || sessions.project.ActivityID != 7 {
		t.Fatalf("project assignment request = %+v", sessions.project)
	}
	if audit.calls != 1 || audit.team != 4 || audit.actor != 12 || audit.action != "session.add" || audit.target != "82" || audit.ip != "" {
		t.Fatalf("historical-session audit = %+v", audit)
	}
}

func TestAddClosedRejectsCallerMismatchBeforePersistence(t *testing.T) {
	sessions := &sessionStarterStub{session: model.Session{ID: 82}}
	audit := &auditRecorderStub{}
	service, err := New(dependencies(sessions, audit))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	_, err = service.AddClosed(requestctx.WithActor(context.Background(), 12), appmodel.TimerAddByIDRequest{
		TeamID: 4, CallerID: 13, ActivityID: 7, Start: start, End: start.Add(time.Hour),
	})
	if !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("AddClosed with caller mismatch error = %v", err)
	}
	if sessions.closed.ActivityID != 0 || audit.calls != 0 {
		t.Fatalf("caller mismatch reached persistence or audit: closed=%+v audit=%+v", sessions.closed, audit)
	}
}

func TestAddClosedActivityRejectsInvalidScopeAndPeriodBeforeWrites(t *testing.T) {
	sessions := &sessionStarterStub{session: model.Session{ID: 82}}
	service, err := New(dependencies(sessions, &auditRecorderStub{}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithActor(context.Background(), 12)
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	request := appmodel.TimerAddByNameRequest{
		TeamID: 4, CallerID: 13, ActivityName: "Deep work", Start: start, End: start.Add(time.Hour),
	}
	if _, _, err := service.AddClosedActivity(ctx, request); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("AddClosedActivity with caller mismatch error = %v", err)
	}
	if sessions.resolved.ID != 0 || sessions.closed.ActivityID != 0 {
		t.Fatalf("caller mismatch reached writes: activity=%+v closed=%+v", sessions.resolved, sessions.closed)
	}

	request.CallerID = 12
	request.End = start
	if _, _, err := service.AddClosedActivity(ctx, request); !errors.Is(err, appmodel.ErrInvalidSessionPeriod) {
		t.Fatalf("AddClosedActivity with invalid period error = %v", err)
	}
	if sessions.resolved.ID != 0 || sessions.closed.ActivityID != 0 {
		t.Fatalf("invalid period reached writes: activity=%+v closed=%+v", sessions.resolved, sessions.closed)
	}
}

func TestStartActivityRejectsMissingTimeBeforeResolvingActivity(t *testing.T) {
	sessions := &sessionStarterStub{session: model.Session{ID: 82}}
	service, err := New(dependencies(sessions, &auditRecorderStub{}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithActor(context.Background(), 12)
	_, _, err = service.StartActivity(ctx, appmodel.TimerStartByNameRequest{
		TeamID: 4, CallerID: 12, ActivityName: "Deep work",
	})
	if !errors.Is(err, appmodel.ErrInvalidSessionStart) {
		t.Fatalf("StartActivity without a timestamp error = %v", err)
	}
	if sessions.resolved.ID != 0 || sessions.startCalls != 0 || sessions.project.ActivityID != 0 {
		t.Fatalf("missing timestamp reached writes: activity=%+v session=%+v project=%+v", sessions.resolved, sessions.session, sessions.project)
	}
}

type auditRecorderStub struct {
	calls  int
	team   int64
	actor  int64
	action string
	target string
	meta   string
	ip     string
}

func TestReopenAndDeleteRecordAuditAfterSuccessfulTransition(t *testing.T) {
	sessions := &sessionStarterStub{session: model.Session{ID: 91}}
	audit := &auditRecorderStub{}
	service, err := New(dependencies(sessions, audit))
	if err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithActor(context.Background(), 12)
	ctx = requestctx.WithClientIP(ctx, "203.0.113.9")
	if _, err := service.Reopen(ctx, appmodel.TimerReopenRequest{TeamID: 4, SessionID: 91, At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if audit.calls != 1 || audit.action != "session.reopen" || audit.actor != 12 || audit.target != "91" || audit.ip != "203.0.113.9" {
		t.Fatalf("reopen audit = %+v", audit)
	}
	if err := service.Delete(ctx, appmodel.SessionDeleteRequest{TeamID: 4, CallerID: 12, SessionID: 91}); err != nil {
		t.Fatal(err)
	}
	if audit.calls != 2 || audit.action != "session.delete" || audit.actor != 12 || audit.target != "91" || audit.ip != "203.0.113.9" {
		t.Fatalf("delete audit = %+v", audit)
	}
}

func TestDeleteRejectsCallerMismatchBeforePersistence(t *testing.T) {
	sessions := &sessionStarterStub{}
	audit := &auditRecorderStub{}
	service, err := New(dependencies(sessions, audit))
	if err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithActor(context.Background(), 12)
	request := appmodel.SessionDeleteRequest{TeamID: 4, CallerID: 13, SessionID: 91}
	if err := service.Delete(ctx, request); err != model.ErrNotFound {
		t.Fatalf("Delete with mismatched caller error = %v, want %v", err, model.ErrNotFound)
	}
	if sessions.deleteCalls != 0 || audit.calls != 0 {
		t.Fatalf("mismatched caller reached persistence or audit: deletes=%d audits=%d", sessions.deleteCalls, audit.calls)
	}
}

func (s *auditRecorderStub) Record(_ context.Context, record model.AuditRecord) error {
	s.calls++
	s.team, s.actor, s.action, s.target, s.meta, s.ip = record.TeamID, record.UserID, record.Action, record.Target, record.Meta, record.IP
	return nil
}

func TestStartRecordsAuditForSuccessfulSession(t *testing.T) {
	sessions := &sessionStarterStub{session: model.Session{ID: 81}}
	audit := &auditRecorderStub{}
	service, err := New(dependencies(sessions, audit))
	if err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithActor(context.Background(), 12)
	ctx = requestctx.WithClientIP(ctx, "203.0.113.8")
	got, err := service.Start(ctx, appmodel.TimerStartRequest{TeamID: 4, ActivityID: 7, At: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Note: "note"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 81 || audit.calls != 1 || audit.team != 4 || audit.actor != 12 || audit.action != "session.start" || audit.target != "81" || audit.meta != "Deep work" || audit.ip != "203.0.113.8" {
		t.Fatalf("session=%+v audit=%+v", got, audit)
	}
}

func TestFocusRecordsStartAuditOnlyWhenItCreatesASession(t *testing.T) {
	sessions := &sessionStarterStub{focus: appmodel.FocusResult{Started: true, StartedSessionID: 82}}
	audit := &auditRecorderStub{}
	service, err := New(dependencies(sessions, audit))
	if err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithActor(context.Background(), 12)
	ctx = requestctx.WithClientIP(ctx, "203.0.113.10")
	got, err := service.Focus(ctx, appmodel.TimerFocusRequest{TeamID: 4, ActivityID: 7, At: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Started || audit.calls != 1 || audit.action != "session.start" || audit.target != "82" || audit.meta != "Deep work" {
		t.Fatalf("focus=%+v audit=%+v", got, audit)
	}

	sessions.focus = appmodel.FocusResult{Paused: 1, Resumed: 1}
	if _, err := service.Focus(ctx, appmodel.TimerFocusRequest{TeamID: 4, ActivityID: 7, At: time.Date(2026, 1, 2, 3, 6, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if audit.calls != 1 {
		t.Fatalf("focus without a new session emitted audit effects: %d", audit.calls)
	}
}

func TestStopCoordinatesGoalAndSessionNotifications(t *testing.T) {
	at := time.Date(2026, time.May, 1, 12, 0, 0, 0, time.UTC)
	sessions := &sessionStarterStub{session: model.Session{
		ID: 91, ActivityID: 7, StartAt: at.Add(-time.Hour), EndAt: &at,
		AccumulatedSeconds: 3600,
	}}
	audit := &auditRecorderStub{}
	notifications := &notificationsStub{}
	deps := dependencies(sessions, audit)
	deps.Goals = &goalProgressReaderStub{goals: []model.GoalProgress{{ActivityName: "Deep work"}}}
	deps.Notifications = notifications
	service, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithActor(context.Background(), 12)
	result, err := service.Stop(ctx, appmodel.TimerStopRequest{TeamID: 4, SessionID: 91, At: at})
	if err != nil {
		t.Fatal(err)
	}
	if result.Session.ID != 91 || result.DurationSeconds != 3600 {
		t.Fatalf("stop result = %+v, want session 91 and duration 3600", result)
	}
	if notifications.stopped != 1 || notifications.achieved != 1 {
		t.Fatalf("notifications = %+v, want stopped and goal-achieved notifications", notifications)
	}
}

func TestStopAllRecordsAuditForEachSession(t *testing.T) {
	at := time.Date(2026, time.May, 1, 12, 0, 0, 0, time.UTC)
	sessions := &sessionStarterStub{stopAllSessions: []model.Session{
		{ID: 91, ActivityID: 7, StartAt: at.Add(-time.Hour), EndAt: &at, AccumulatedSeconds: 600},
		{ID: 92, ActivityID: 7, StartAt: at.Add(-time.Minute), EndAt: &at, AccumulatedSeconds: 600},
	}}
	audit := &auditRecorderStub{}
	goalReader := &goalProgressReaderStub{goals: []model.GoalProgress{{ActivityName: "Deep work"}}}
	notifications := &notificationsStub{}
	deps := dependencies(sessions, audit)
	deps.Goals, deps.Notifications = goalReader, notifications
	service, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := service.StopAll(requestctx.WithActor(context.Background(), 12), appmodel.TimerStopAllRequest{TeamID: 4, At: at})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != 91 || ids[1] != 92 {
		t.Fatalf("stopped IDs = %v, want [91 92]", ids)
	}
	if audit.calls != 2 {
		t.Fatalf("stop-all audit calls = %d", audit.calls)
	}
	if goalReader.stoppedBatch[7] != 1200 || notifications.achieved != 1 {
		t.Fatalf("stop-all goal handling = durations %+v, notifications %+v", goalReader.stoppedBatch, notifications)
	}
}
