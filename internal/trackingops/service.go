// Package trackingops coordinates timer transitions with audit, webhook, and
// user-notification effects shared by the application transports.
package trackingops

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/postcommit"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

var ErrIncompleteDependencies = errors.New("tracking operations dependencies are incomplete")

type SessionStarter interface {
	Start(context.Context, appmodel.TimerStartRequest) (model.Session, error)
	Focus(context.Context, appmodel.TimerFocusRequest) (appmodel.FocusResult, error)
	Stop(context.Context, appmodel.TimerStopRequest) (model.Session, error)
	StopAll(context.Context, appmodel.TimerStopAllRequest) ([]model.Session, error)
	Reopen(context.Context, appmodel.TimerReopenRequest) (model.Session, error)
	Delete(context.Context, appmodel.SessionDeleteRequest) error
}

type AuditRecorder interface {
	Record(context.Context, model.AuditRecord) error
}

type ActivityReader interface {
	Activity(context.Context, int64, int64) (model.Activity, error)
}

type ActivityResolver interface {
	ResolveActivityForMember(context.Context, appmodel.ActivityResolveRequest) (model.Activity, error)
}

type ProjectAssigner interface {
	AssignActivity(context.Context, appmodel.AssignActivityProjectRequest) error
}

type ClosedSessionCreator interface {
	AddClosed(context.Context, appmodel.TimerAddRequest) (model.Session, error)
}

type GoalProgressReader interface {
	NewlyAchievedAfterSessions(context.Context, int64, map[int64]int, time.Time) ([]model.GoalProgress, error)
}

type Notifications interface {
	SessionStopped(context.Context, appmodel.SessionStoppedNotification) error
	GoalAchieved(context.Context, appmodel.GoalAchievedNotification) error
}

type Logger interface {
	Printf(string, ...any)
}

type Dependencies struct {
	Sessions       SessionStarter
	Activities     ActivityReader
	Resolver       ActivityResolver
	Projects       ProjectAssigner
	ClosedSessions ClosedSessionCreator
	Goals          GoalProgressReader
	Audit          AuditRecorder
	Notifications  Notifications
	Logger         Logger
}

type Service struct {
	sessions       SessionStarter
	activities     ActivityReader
	resolver       ActivityResolver
	projects       ProjectAssigner
	closedSessions ClosedSessionCreator
	goals          GoalProgressReader
	audit          AuditRecorder
	notifications  Notifications
	logger         Logger
}

func New(deps Dependencies) (*Service, error) {
	for _, dependency := range []struct {
		name string
		port any
	}{
		{"session starter", deps.Sessions},
		{"activity reader", deps.Activities},
		{"activity resolver", deps.Resolver},
		{"project assignment", deps.Projects},
		{"closed session creator", deps.ClosedSessions},
		{"goal progress reader", deps.Goals},
		{"audit recorder", deps.Audit},
		{"notifications", deps.Notifications},
		{"logger", deps.Logger},
	} {
		if depcheck.IsNil(dependency.port) {
			return nil, fmt.Errorf("%w: %s", ErrIncompleteDependencies, dependency.name)
		}
	}
	return &Service{
		sessions: deps.Sessions, activities: deps.Activities, goals: deps.Goals,
		resolver: deps.Resolver, projects: deps.Projects,
		closedSessions: deps.ClosedSessions,
		audit:          deps.Audit, notifications: deps.Notifications,
		logger: deps.Logger,
	}, nil
}

// StartActivity resolves an activity, optionally assigns its first project,
// and starts tracking through the same audited transition used by other
// callers. Keeping this sequence here gives HTTP and CLI one use-case path.
func (s *Service) StartActivity(ctx context.Context, request appmodel.TimerStartByNameRequest) (model.Activity, model.Session, error) {
	name := strings.TrimSpace(request.ActivityName)
	if request.TeamID <= 0 || name == "" || request.At.IsZero() {
		return model.Activity{}, model.Session{}, appmodel.ErrInvalidSessionStart
	}
	if request.CallerID <= 0 || request.CallerID != requestctx.ActorID(ctx) {
		return model.Activity{}, model.Session{}, model.ErrForbidden
	}
	activity, err := s.resolveActivity(ctx, request.TeamID, request.CallerID, name, request.ProjectID)
	if err != nil {
		return model.Activity{}, model.Session{}, err
	}
	session, err := s.Start(ctx, appmodel.TimerStartRequest{
		TeamID: request.TeamID, ActivityID: activity.ID, At: request.At, Note: request.Note,
	})
	if err != nil {
		return activity, model.Session{}, err
	}
	return activity, session, nil
}

// AddClosedActivity resolves an activity, applies an optional project, and
// creates its historical session as one application use case.
func (s *Service) AddClosedActivity(ctx context.Context, request appmodel.TimerAddByNameRequest) (model.Activity, model.Session, error) {
	name := strings.TrimSpace(request.ActivityName)
	if request.TeamID <= 0 || name == "" {
		return model.Activity{}, model.Session{}, appmodel.ErrInvalidSessionStart
	}
	if err := validateClosedSessionRequest(ctx, request.CallerID, request.Start, request.End); err != nil {
		return model.Activity{}, model.Session{}, err
	}
	activity, err := s.resolveActivity(ctx, request.TeamID, request.CallerID, name, request.ProjectID)
	if err != nil {
		return activity, model.Session{}, err
	}
	session, err := s.AddClosed(ctx, appmodel.TimerAddByIDRequest{
		TeamID: request.TeamID, CallerID: request.CallerID, ActivityID: activity.ID,
		Start: request.Start, End: request.End, Note: request.Note,
	})
	if err != nil {
		return activity, model.Session{}, err
	}
	return activity, session, nil
}

// AddClosed persists a historical interval and records its audit effect. Both
// transports use this path so actor validation and auditing stay consistent.
func (s *Service) AddClosed(ctx context.Context, request appmodel.TimerAddByIDRequest) (model.Session, error) {
	if request.TeamID <= 0 || request.ActivityID <= 0 {
		return model.Session{}, appmodel.ErrInvalidSessionStart
	}
	if err := validateClosedSessionRequest(ctx, request.CallerID, request.Start, request.End); err != nil {
		return model.Session{}, err
	}
	session, err := s.closedSessions.AddClosed(ctx, appmodel.TimerAddRequest{
		TeamID: request.TeamID, ActivityID: request.ActivityID,
		Start: request.Start, End: request.End, Note: request.Note,
	})
	if err != nil {
		return model.Session{}, err
	}
	effectCtx, cancel := postcommit.NewContext(ctx)
	defer cancel()
	if err := s.audit.Record(effectCtx, model.AuditRecord{
		TeamID: request.TeamID, UserID: requestctx.ActorID(ctx), Action: "session.add",
		Target: strconv.FormatInt(session.ID, 10), IP: requestctx.ClientIP(ctx),
	}); err != nil {
		s.logger.Printf("tracking: record historical session audit for session %d: %v", session.ID, err)
	}
	return session, nil
}

func validateClosedSessionRequest(ctx context.Context, callerID int64, start, end time.Time) error {
	if callerID <= 0 || callerID != requestctx.ActorID(ctx) {
		return model.ErrForbidden
	}
	if start.IsZero() {
		return appmodel.ErrInvalidSessionStart
	}
	if !end.After(start) {
		return appmodel.ErrInvalidSessionPeriod
	}
	if end.Sub(start) > time.Duration(model.MaxSessionDurationSeconds)*time.Second {
		return model.ErrSessionDurationOverflow
	}
	return nil
}

func (s *Service) resolveActivity(ctx context.Context, teamID, callerID int64, name string, projectID int64) (model.Activity, error) {
	activity, err := s.resolver.ResolveActivityForMember(ctx, appmodel.ActivityResolveRequest{
		TeamID: teamID, CallerID: callerID, Name: name,
	})
	if err != nil {
		return model.Activity{}, err
	}
	if projectID <= 0 {
		return activity, nil
	}
	if err := s.projects.AssignActivity(ctx, appmodel.AssignActivityProjectRequest{
		TeamID: teamID, ActivityID: activity.ID,
		ProjectID: projectID, CallerID: callerID,
	}); err != nil {
		return activity, err
	}
	activity.ProjectID = projectID
	return activity, nil
}

// Start begins tracking and publishes side effects once the session write
// succeeds. HTTP and API callers share the same audit and event behavior.
func (s *Service) Start(ctx context.Context, request appmodel.TimerStartRequest) (model.Session, error) {
	if request.TeamID <= 0 || request.ActivityID <= 0 {
		return model.Session{}, model.ErrNotFound
	}
	teamID := request.TeamID
	session, err := s.sessions.Start(ctx, request)
	if err != nil {
		return model.Session{}, err
	}
	effectCtx, cancel := postcommit.NewContext(ctx)
	defer cancel()
	activityName := ""
	if activity, err := s.activities.Activity(effectCtx, teamID, request.ActivityID); err != nil {
		s.logger.Printf("tracking: load activity name for session %d audit: %v", session.ID, err)
	} else {
		activityName = activity.Name
	}
	if err := s.audit.Record(effectCtx, model.AuditRecord{
		TeamID: teamID, UserID: requestctx.ActorID(ctx), Action: "session.start", Target: strconv.FormatInt(session.ID, 10), Meta: activityName, IP: requestctx.ClientIP(ctx),
	}); err != nil {
		s.logger.Printf("tracking: record session start audit for session %d: %v", session.ID, err)
	}
	return session, nil
}

// Focus atomically changes the active set through tracking and publishes the
// normal start effects when focus creates a new session.
func (s *Service) Focus(ctx context.Context, request appmodel.TimerFocusRequest) (appmodel.FocusResult, error) {
	if request.TeamID <= 0 || request.ActivityID <= 0 {
		return appmodel.FocusResult{}, model.ErrNotFound
	}
	teamID := request.TeamID
	result, err := s.sessions.Focus(ctx, request)
	if err != nil {
		return appmodel.FocusResult{}, err
	}
	if !result.Started {
		return result, nil
	}
	effectCtx, cancel := postcommit.NewContext(ctx)
	defer cancel()
	target := strconv.FormatInt(result.StartedSessionID, 10)
	activityName := ""
	if activity, err := s.activities.Activity(effectCtx, teamID, request.ActivityID); err != nil {
		s.logger.Printf("tracking: load activity name for focused session %s: %v", target, err)
	} else {
		activityName = activity.Name
	}
	if err := s.audit.Record(effectCtx, model.AuditRecord{
		TeamID: teamID, UserID: requestctx.ActorID(ctx), Action: "session.start", Target: target, Meta: activityName, IP: requestctx.ClientIP(ctx),
	}); err != nil {
		s.logger.Printf("tracking: record focused session audit for session %s: %v", target, err)
	}
	return result, nil
}

// Stop ends one session, then records audit and notification effects.
func (s *Service) Stop(ctx context.Context, request appmodel.TimerStopRequest) (appmodel.TimerStopResult, error) {
	if request.TeamID <= 0 || request.SessionID <= 0 {
		return appmodel.TimerStopResult{}, model.ErrNotFound
	}
	teamID, sessionID, at := request.TeamID, request.SessionID, request.At
	session, err := s.sessions.Stop(ctx, request)
	if err != nil {
		return appmodel.TimerStopResult{}, err
	}
	duration := session.DurationSeconds(at)
	effectCtx, cancel := postcommit.NewContext(ctx)
	defer cancel()
	target := strconv.FormatInt(sessionID, 10)
	if err := s.audit.Record(effectCtx, model.AuditRecord{
		TeamID: teamID, UserID: requestctx.ActorID(ctx), Action: "session.stop", Target: target, IP: requestctx.ClientIP(ctx),
	}); err != nil {
		s.logger.Printf("tracking: record session stop audit for session %d: %v", sessionID, err)
	}
	activityName := "a session"
	if activity, err := s.activities.Activity(effectCtx, teamID, session.ActivityID); err == nil {
		activityName = activity.Name
	} else {
		s.logger.Printf("tracking: load activity name for stopped session %s: %v", target, err)
	}
	if err := s.notifications.SessionStopped(effectCtx, appmodel.SessionStoppedNotification{
		TeamID: teamID, UserID: requestctx.ActorID(ctx), ActivityName: activityName,
	}); err != nil {
		s.logger.Printf("tracking: enqueue stopped notification for session %d: %v", sessionID, err)
	}
	s.notifyGoalsAfterSessions(effectCtx, teamID, map[int64]int{session.ActivityID: duration}, at, strconv.FormatInt(sessionID, 10))
	return appmodel.TimerStopResult{Session: session, DurationSeconds: duration, ActivityName: activityName}, nil
}

// StopAll ends every active session at one instant and records its audit
// effects before returning IDs to the transport. Persistence records webhooks
// in the same transaction as those transitions.
func (s *Service) StopAll(ctx context.Context, request appmodel.TimerStopAllRequest) ([]int64, error) {
	if request.TeamID <= 0 {
		return nil, model.ErrNotFound
	}
	teamID, at := request.TeamID, request.At
	sessions, err := s.sessions.StopAll(ctx, request)
	if err != nil {
		return nil, err
	}
	effectCtx, cancel := postcommit.NewContext(ctx)
	defer cancel()
	ids := make([]int64, 0, len(sessions))
	stoppedSeconds := make(map[int64]int)
	for _, session := range sessions {
		ids = append(ids, session.ID)
		stoppedSeconds[session.ActivityID] += session.DurationSeconds(at)
		if err := s.audit.Record(effectCtx, model.AuditRecord{
			TeamID: teamID, UserID: requestctx.ActorID(ctx), Action: "session.stop", Target: strconv.FormatInt(session.ID, 10), Meta: "stop-all", IP: requestctx.ClientIP(ctx),
		}); err != nil {
			s.logger.Printf("tracking: record stop-all audit for session %d: %v", session.ID, err)
		}
	}
	s.notifyGoalsAfterSessions(effectCtx, teamID, stoppedSeconds, at, "stop-all")
	return ids, nil
}

func (s *Service) notifyGoalsAfterSessions(ctx context.Context, teamID int64, stoppedSeconds map[int64]int, at time.Time, target string) {
	if len(stoppedSeconds) == 0 {
		return
	}
	goals, err := s.goals.NewlyAchievedAfterSessions(ctx, teamID, stoppedSeconds, at)
	if err != nil {
		s.logger.Printf("tracking: check goal progress after %s: %v", target, err)
		return
	}
	for _, goal := range goals {
		if err := s.notifications.GoalAchieved(ctx, appmodel.GoalAchievedNotification{
			TeamID: teamID, UserID: requestctx.ActorID(ctx), Goal: goal,
		}); err != nil {
			s.logger.Printf("tracking: enqueue goal achievement notification after %s: %v", target, err)
		}
	}
}

// Reopen restores a recently stopped session and records the successful
// transition within the shared tracking operation boundary.
func (s *Service) Reopen(ctx context.Context, request appmodel.TimerReopenRequest) (model.Session, error) {
	if request.TeamID <= 0 || request.SessionID <= 0 {
		return model.Session{}, model.ErrNotFound
	}
	teamID := request.TeamID
	session, err := s.sessions.Reopen(ctx, request)
	if err != nil {
		return model.Session{}, err
	}
	effectCtx, cancel := postcommit.NewContext(ctx)
	defer cancel()
	if err := s.audit.Record(effectCtx, model.AuditRecord{
		TeamID: teamID, UserID: requestctx.ActorID(ctx), Action: "session.reopen", Target: strconv.FormatInt(session.ID, 10), IP: requestctx.ClientIP(ctx),
	}); err != nil {
		s.logger.Printf("tracking: record session reopen audit for session %d: %v", session.ID, err)
	}
	return session, nil
}

// Delete removes a session and records the audit only after persistence
// confirms the scoped deletion.
func (s *Service) Delete(ctx context.Context, request appmodel.SessionDeleteRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.SessionID <= 0 || request.CallerID != requestctx.ActorID(ctx) {
		return model.ErrNotFound
	}
	if err := s.sessions.Delete(ctx, request); err != nil {
		return err
	}
	teamID, sessionID := request.TeamID, request.SessionID
	effectCtx, cancel := postcommit.NewContext(ctx)
	defer cancel()
	if err := s.audit.Record(effectCtx, model.AuditRecord{
		TeamID: teamID, UserID: requestctx.ActorID(ctx), Action: "session.delete", Target: strconv.FormatInt(sessionID, 10), IP: requestctx.ClientIP(ctx),
	}); err != nil {
		s.logger.Printf("tracking: record session delete audit for session %d: %v", sessionID, err)
	}
	return nil
}
