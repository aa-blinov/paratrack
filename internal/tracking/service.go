// Package tracking contains application workflows for live time tracking.
// Transport adapters (CLI and HTTP) own parsing and presentation; this
// package coordinates active-session transitions through a narrow store.
package tracking

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
)

var (
	ErrAlreadyRunning   = model.ErrActiveSessionExists
	ErrInvalidStart     = appmodel.ErrInvalidSessionStart
	ErrSessionNotActive = model.ErrSessionNotActive
	ErrAlreadyPaused    = model.ErrSessionAlreadyPaused
	ErrNotPaused        = model.ErrSessionNotPaused
	ErrReopenExpired    = model.ErrSessionReopenExpired
	ErrInvalidEdit      = appmodel.ErrInvalidSessionEdit
	ErrInvalidDelete    = appmodel.ErrInvalidSessionDelete
	ErrInvalidInterval  = appmodel.ErrInvalidSessionPeriod
	ErrInvalidDuration  = appmodel.ErrInvalidSessionLength
)

const ReopenWindow = 10 * time.Minute

// SessionStore provides scoped session commands.
type SessionStore interface {
	StartSession(context.Context, appmodel.TimerStartRequest) (model.Session, error)
	FocusActivity(context.Context, appmodel.TimerFocusRequest) (appmodel.FocusResult, error)
	StopActiveSessions(context.Context, appmodel.TimerStopAllRequest) ([]model.Session, error)
	PauseActiveSessions(context.Context, appmodel.TimerStopAllRequest) ([]int64, error)
	UpdateSessionEnd(context.Context, appmodel.TimerStopRequest) (model.Session, error)
	PauseSession(context.Context, appmodel.TimerSessionRequest) (model.Session, error)
	ResumeSession(context.Context, appmodel.TimerSessionRequest) (model.Session, error)
	ReopenSession(context.Context, appmodel.TimerReopenRequest) (model.Session, error)
	UpdateSessionFields(context.Context, appmodel.SessionUpdateRequest) error
	DeleteSession(context.Context, appmodel.SessionDeleteRequest) error
	CreateClosedSession(context.Context, appmodel.TimerAddRequest) (model.Session, error)
}

// SessionQueryStore provides scoped session history and summary reads.
type SessionQueryStore interface {
	GetSession(context.Context, appmodel.SessionLookupQuery) (model.Session, error)
	ListActiveSessions(context.Context, int64) ([]appmodel.ActiveSession, error)
	ListClosedSessions(context.Context, appmodel.ClosedSessionsQuery) ([]appmodel.ActiveSession, error)
	HasAnySession(context.Context, int64) (bool, error)
	ListSessionsPage(context.Context, appmodel.SessionHistoryPageQuery) ([]appmodel.ActiveSession, bool, error)
}

// ActivityStore provides activity lookup and lifecycle operations.
type ActivityStore interface {
	GetOrCreateActivityForMember(context.Context, appmodel.ActivityResolveRequest) (model.Activity, error)
	FindActivityByName(context.Context, appmodel.ActivityNameQuery) (model.Activity, error)
	ListActivities(context.Context, int64, bool) ([]model.Activity, error)
	GetActivity(context.Context, appmodel.ActivityLookupQuery) (model.Activity, error)
}

// TimesheetStore provides grid reads and atomic daily-total replacement.
type TimesheetStore interface {
	UpsertDayTotal(context.Context, appmodel.TimesheetCellUpdateRequest) error
	ListTimesheet(context.Context, appmodel.TimesheetRequest) (appmodel.TimesheetWeek, error)
}

// Dependencies keeps query, command, activity and timesheet concerns on
// independently substitutable persistence ports.
type Dependencies struct {
	Sessions   SessionStore
	Queries    SessionQueryStore
	Activities ActivityStore
	Timesheets TimesheetStore
}

var ErrIncompleteDependencies = errors.New("tracking service dependencies are incomplete")

// Service coordinates application-level live-session rules.
type Service struct {
	sessions   SessionStore
	queries    SessionQueryStore
	activities ActivityStore
	timesheets TimesheetStore
}

func New(deps Dependencies) (*Service, error) {
	missing := []struct {
		name string
		port any
	}{
		{"sessions", deps.Sessions}, {"session queries", deps.Queries},
		{"activities", deps.Activities}, {"timesheets", deps.Timesheets},
	}
	for _, dependency := range missing {
		if depcheck.IsNil(dependency.port) {
			return nil, fmt.Errorf("%w: %s", ErrIncompleteDependencies, dependency.name)
		}
	}
	return &Service{
		sessions: deps.Sessions, queries: deps.Queries,
		activities: deps.Activities, timesheets: deps.Timesheets,
	}, nil
}

// ResolveActivityForMember resolves or creates an activity through a
// persistence operation that rechecks current workspace membership.
func (s *Service) ResolveActivityForMember(ctx context.Context, request appmodel.ActivityResolveRequest) (model.Activity, error) {
	name := strings.TrimSpace(request.Name)
	if request.TeamID <= 0 || request.CallerID <= 0 || name == "" {
		return model.Activity{}, ErrInvalidStart
	}
	request.Name = name
	activity, err := s.activities.GetOrCreateActivityForMember(ctx, request)
	if err != nil {
		return model.Activity{}, fmt.Errorf("resolve workspace activity: %w", err)
	}
	return activity, nil
}

// FindActivity looks up an existing activity without creating one. Focus in
// the HTTP UI uses this behavior so a miss remains a not-found response.
func (s *Service) FindActivity(ctx context.Context, query appmodel.ActivityNameQuery) (model.Activity, error) {
	query.Name = strings.TrimSpace(query.Name)
	if query.TeamID <= 0 || query.Name == "" {
		return model.Activity{}, ErrInvalidStart
	}
	activity, err := s.activities.FindActivityByName(ctx, query)
	if err != nil {
		return model.Activity{}, fmt.Errorf("find activity: %w", err)
	}
	return activity, nil
}

// Activities lists workspace activities for command and page selectors.
func (s *Service) Activities(ctx context.Context, teamID int64, includeArchived bool) ([]model.Activity, error) {
	if teamID <= 0 {
		return nil, ErrInvalidStart
	}
	activities, err := s.activities.ListActivities(ctx, teamID, includeArchived)
	if err != nil {
		return nil, fmt.Errorf("list activities: %w", err)
	}
	return activities, nil
}

// AddClosed creates a historical session after enforcing a valid time range.
func (s *Service) AddClosed(ctx context.Context, request appmodel.TimerAddRequest) (model.Session, error) {
	if request.TeamID <= 0 || request.ActivityID <= 0 || request.Start.IsZero() {
		return model.Session{}, ErrInvalidStart
	}
	start, end := request.Start, request.End
	if !end.After(start) {
		return model.Session{}, ErrInvalidInterval
	}
	if end.Sub(start) > time.Duration(model.MaxSessionDurationSeconds)*time.Second {
		return model.Session{}, model.ErrSessionDurationOverflow
	}
	session, err := s.sessions.CreateClosedSession(ctx, request)
	if err != nil {
		return model.Session{}, fmt.Errorf("add closed session: %w", err)
	}
	return session, nil
}

// SetDayTotal replaces the current actor's timesheet cell. The persistence
// adapter applies the change atomically and refuses cells billed on a sent
// or paid invoice.
func (s *Service) SetDayTotal(ctx context.Context, request appmodel.TimesheetCellUpdateRequest) error {
	if request.TeamID <= 0 || request.ActivityID <= 0 || request.Day.IsZero() || request.TotalSeconds < 0 || request.TotalSeconds > 24*60*60 {
		return ErrInvalidEdit
	}
	if err := s.timesheets.UpsertDayTotal(ctx, request); err != nil {
		return fmt.Errorf("update timesheet cell: %w", err)
	}
	return nil
}

// Timesheet loads a team's weekly grid, including requested extra activity
// rows that are not otherwise present in the week.
func (s *Service) Timesheet(ctx context.Context, request appmodel.TimesheetRequest) (appmodel.TimesheetWeek, error) {
	if request.TeamID <= 0 || request.WeekStart.IsZero() || request.Now.IsZero() {
		return appmodel.TimesheetWeek{}, ErrInvalidEdit
	}
	for _, activityID := range request.ExtraActivityIDs {
		if activityID <= 0 {
			return appmodel.TimesheetWeek{}, ErrInvalidEdit
		}
	}
	grid, err := s.timesheets.ListTimesheet(ctx, request)
	if err != nil {
		return appmodel.TimesheetWeek{}, fmt.Errorf("load timesheet grid: %w", err)
	}
	return grid, nil
}

func (s *Service) ActiveSessions(ctx context.Context, teamID int64) ([]appmodel.ActiveSession, error) {
	if teamID <= 0 {
		return nil, ErrInvalidStart
	}
	sessions, err := s.queries.ListActiveSessions(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("list active sessions: %w", err)
	}
	return sessions, nil
}

func (s *Service) ClosedSessions(ctx context.Context, query appmodel.ClosedSessionsQuery) ([]appmodel.ActiveSession, error) {
	if query.TeamID <= 0 || query.Start.IsZero() || query.End.Before(query.Start) ||
		(query.ActivityID != nil && *query.ActivityID <= 0) || (query.ProjectID != nil && *query.ProjectID <= 0) {
		return nil, ErrInvalidInterval
	}
	sessions, err := s.queries.ListClosedSessions(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list closed sessions: %w", err)
	}
	return sessions, nil
}

func (s *Service) HasAnySession(ctx context.Context, teamID int64) (bool, error) {
	if teamID <= 0 {
		return false, ErrInvalidStart
	}
	seen, err := s.queries.HasAnySession(ctx, teamID)
	if err != nil {
		return false, fmt.Errorf("check for prior sessions: %w", err)
	}
	return seen, nil
}

func (s *Service) Activity(ctx context.Context, teamID, activityID int64) (model.Activity, error) {
	if teamID <= 0 || activityID <= 0 {
		return model.Activity{}, ErrInvalidStart
	}
	activity, err := s.activities.GetActivity(ctx, appmodel.ActivityLookupQuery{TeamID: teamID, ActivityID: activityID})
	if err != nil {
		return model.Activity{}, fmt.Errorf("get activity: %w", err)
	}
	return activity, nil
}

func (s *Service) Session(ctx context.Context, teamID, sessionID int64) (model.Session, error) {
	if teamID <= 0 || sessionID <= 0 {
		return model.Session{}, ErrInvalidStart
	}
	session, err := s.queries.GetSession(ctx, appmodel.SessionLookupQuery{TeamID: teamID, SessionID: sessionID})
	if err != nil {
		return model.Session{}, fmt.Errorf("get session: %w", err)
	}
	return session, nil
}

func (s *Service) SessionHistoryPage(ctx context.Context, query appmodel.SessionHistoryPageQuery) (appmodel.SessionPage, error) {
	if query.TeamID <= 0 || query.From.IsZero() || query.To.IsZero() || query.To.Before(query.From) {
		return appmodel.SessionPage{}, ErrInvalidInterval
	}
	if query.Limit <= 0 {
		query.Limit = 100
	}
	if query.Limit > 500 {
		query.Limit = 500
	}
	if query.After != nil {
		start, err := time.Parse(time.RFC3339Nano, query.After.Start)
		if err != nil || query.After.ID <= 0 {
			return appmodel.SessionPage{}, fmt.Errorf("invalid session cursor")
		}
		canonical := start.UTC().Format(time.RFC3339Nano)
		query.After = &appmodel.SessionCursor{Start: canonical, ID: query.After.ID}
	}
	items, more, err := s.queries.ListSessionsPage(ctx, query)
	if err != nil {
		return appmodel.SessionPage{}, fmt.Errorf("list paginated session history: %w", err)
	}
	page := appmodel.SessionPage{Items: items, HasMore: more}
	if more && len(items) > 0 {
		last := items[len(items)-1].Session
		page.NextCursor = &appmodel.SessionCursor{Start: last.StartAt.UTC().Format(time.RFC3339Nano), ID: last.ID}
	}
	return page, nil
}

// Start creates a timer unless that user already has an open session for
// the activity. Paused sessions are still open and therefore count.
func (s *Service) Start(ctx context.Context, request appmodel.TimerStartRequest) (model.Session, error) {
	if request.TeamID <= 0 || request.ActivityID <= 0 {
		return model.Session{}, fmt.Errorf("%w: team and activity IDs must be valid", ErrInvalidStart)
	}
	if request.At.IsZero() {
		return model.Session{}, fmt.Errorf("%w: start time is required", ErrInvalidStart)
	}
	session, err := s.sessions.StartSession(ctx, request)
	if err != nil {
		return model.Session{}, fmt.Errorf("start session: %w", err)
	}
	return session, nil
}

// Focus atomically pauses the user's other running sessions and resumes or
// starts a session for the selected activity.
func (s *Service) Focus(ctx context.Context, request appmodel.TimerFocusRequest) (appmodel.FocusResult, error) {
	if request.TeamID <= 0 || request.ActivityID <= 0 {
		return appmodel.FocusResult{}, fmt.Errorf("%w: team and activity IDs must be valid", ErrInvalidStart)
	}
	if request.At.IsZero() {
		return appmodel.FocusResult{}, fmt.Errorf("%w: focus time is required", ErrInvalidStart)
	}
	result, err := s.sessions.FocusActivity(ctx, request)
	if err != nil {
		return appmodel.FocusResult{}, fmt.Errorf("focus activity: %w", err)
	}
	return result, nil
}

// StopAll atomically closes every open session in the current actor/workspace
// scope and returns the persisted session snapshots to its operation coordinator.
func (s *Service) StopAll(ctx context.Context, request appmodel.TimerStopAllRequest) ([]model.Session, error) {
	if request.TeamID <= 0 {
		return nil, fmt.Errorf("%w: team ID must be valid", ErrInvalidStart)
	}
	if request.At.IsZero() {
		return nil, fmt.Errorf("%w: stop time is required", ErrInvalidStart)
	}
	sessions, err := s.sessions.StopActiveSessions(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("stop all sessions: %w", err)
	}
	return sessions, nil
}

// PauseAll atomically pauses every running session in the actor/workspace
// scope and returns the affected session IDs.
func (s *Service) PauseAll(ctx context.Context, request appmodel.TimerStopAllRequest) ([]int64, error) {
	if request.TeamID <= 0 {
		return nil, fmt.Errorf("%w: team ID must be valid", ErrInvalidStart)
	}
	if request.At.IsZero() {
		return nil, fmt.Errorf("%w: pause time is required", ErrInvalidStart)
	}
	ids, err := s.sessions.PauseActiveSessions(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("pause all sessions: %w", err)
	}
	return ids, nil
}

func (s *Service) Stop(ctx context.Context, request appmodel.TimerStopRequest) (model.Session, error) {
	if request.TeamID <= 0 || request.SessionID <= 0 || request.At.IsZero() {
		return model.Session{}, fmt.Errorf("%w: invalid stop request", ErrInvalidStart)
	}
	session, err := s.sessions.UpdateSessionEnd(ctx, request)
	if err != nil {
		return model.Session{}, fmt.Errorf("stop session: %w", err)
	}
	return session, nil
}

func (s *Service) Pause(ctx context.Context, request appmodel.TimerSessionRequest) (model.Session, error) {
	if request.TeamID <= 0 || request.SessionID <= 0 || request.At.IsZero() {
		return model.Session{}, fmt.Errorf("%w: invalid pause request", ErrInvalidStart)
	}
	session, err := s.sessions.PauseSession(ctx, request)
	if err != nil {
		return model.Session{}, fmt.Errorf("pause session: %w", err)
	}
	if session.EndAt != nil {
		return model.Session{}, ErrSessionNotActive
	}
	if session.Paused {
		return model.Session{}, ErrAlreadyPaused
	}
	return session, nil
}

func (s *Service) Resume(ctx context.Context, request appmodel.TimerSessionRequest) (model.Session, error) {
	if request.TeamID <= 0 || request.SessionID <= 0 || request.At.IsZero() {
		return model.Session{}, fmt.Errorf("%w: invalid resume request", ErrInvalidStart)
	}
	session, err := s.sessions.ResumeSession(ctx, request)
	if err != nil {
		return model.Session{}, fmt.Errorf("resume session: %w", err)
	}
	if session.EndAt != nil {
		return model.Session{}, ErrSessionNotActive
	}
	if !session.Paused {
		return model.Session{}, ErrNotPaused
	}
	return session, nil
}

func (s *Service) Reopen(ctx context.Context, request appmodel.TimerReopenRequest) (model.Session, error) {
	if request.TeamID <= 0 || request.SessionID <= 0 || request.At.IsZero() {
		return model.Session{}, fmt.Errorf("%w: invalid reopen request", ErrInvalidStart)
	}
	teamID, sessionID, now := request.TeamID, request.SessionID, request.At
	current, err := s.queries.GetSession(ctx, appmodel.SessionLookupQuery{TeamID: teamID, SessionID: sessionID})
	if err != nil {
		return model.Session{}, fmt.Errorf("load session to reopen: %w", err)
	}
	if current.EndAt == nil || now.Before(*current.EndAt) || now.Sub(*current.EndAt) > ReopenWindow {
		return model.Session{}, ErrReopenExpired
	}
	request.ExpectedEndAt = *current.EndAt
	session, err := s.sessions.ReopenSession(ctx, request)
	if err != nil {
		return model.Session{}, fmt.Errorf("reopen session: %w", err)
	}
	return session, nil
}

// UpdateFields edits a session only while its invoice remains a draft. The
// store enforces this under row locks so concurrent invoice sends cannot race
// the edit.
func (s *Service) UpdateFields(ctx context.Context, request appmodel.SessionUpdateRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.SessionID <= 0 || request.Update.UpdatedAt.IsZero() {
		return ErrInvalidEdit
	}
	if request.DurationSeconds != nil {
		if *request.DurationSeconds < 0 {
			return ErrInvalidDuration
		}
		if int64(*request.DurationSeconds) > model.MaxSessionDurationSeconds {
			return model.ErrSessionDurationOverflow
		}
	}
	if request.Update.AccumulatedSeconds != nil {
		if *request.Update.AccumulatedSeconds < 0 {
			return ErrInvalidDuration
		}
		if int64(*request.Update.AccumulatedSeconds) > model.MaxSessionDurationSeconds {
			return model.ErrSessionDurationOverflow
		}
	}
	if err := s.sessions.UpdateSessionFields(ctx, request); err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	return nil
}

// Delete removes an editable session. The store checks invoice state and
// serializes the delete with invoice status changes.
func (s *Service) Delete(ctx context.Context, request appmodel.SessionDeleteRequest) error {
	if request.TeamID <= 0 {
		return ErrInvalidDelete
	}
	actorID, sessionID := request.CallerID, request.SessionID
	if actorID <= 0 || sessionID <= 0 {
		return ErrInvalidDelete
	}
	if err := s.sessions.DeleteSession(ctx, request); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}
