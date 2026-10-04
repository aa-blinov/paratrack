// Package dashboard assembles the read model shared by the dashboard UI and
// any future dashboard transport.
package dashboard

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
)

type GoalReader interface {
	Activities(context.Context, int64) ([]model.Activity, error)
	Progress(context.Context, appmodel.GoalProgressQuery) ([]appmodel.GoalProgress, error)
}

type TrackingReader interface {
	ActiveSessions(context.Context, int64) ([]model.ActiveSession, error)
	ClosedSessions(context.Context, appmodel.ClosedSessionsQuery) ([]model.ActiveSession, error)
	HasAnySession(context.Context, int64) (bool, error)
}

type ProjectReader interface {
	List(context.Context, appmodel.ProjectCatalogQuery) ([]model.Project, error)
}

type SessionDecorationBuilder interface {
	Build(context.Context, appmodel.SessionDecorationRequest) (appmodel.SessionDecorationSnapshot, error)
}

type InvoiceReader interface {
	UnbilledProjectTime(context.Context, appmodel.UnbilledProjectQuery) ([]model.UnbilledProject, error)
}

type Logger interface {
	Printf(string, ...any)
}

type Dependencies struct {
	Goals       GoalReader
	Tracking    TrackingReader
	Projects    ProjectReader
	Decorations SessionDecorationBuilder
	Invoices    InvoiceReader
	Logger      Logger
}

var (
	ErrIncompleteDependencies = errors.New("dashboard builder dependencies are incomplete")
	ErrInvalidQuery           = errors.New("invalid dashboard query")
)

type Builder struct {
	goals       GoalReader
	tracking    TrackingReader
	projects    ProjectReader
	decorations SessionDecorationBuilder
	invoices    InvoiceReader
	logger      Logger
}

type Query = appmodel.DashboardQuery
type Snapshot = appmodel.DashboardSnapshot

func NewBuilder(deps Dependencies) (*Builder, error) {
	missing := []struct {
		name string
		port any
	}{
		{"goals", deps.Goals}, {"tracking", deps.Tracking},
		{"projects", deps.Projects}, {"session decorations", deps.Decorations}, {"invoices", deps.Invoices},
		{"logger", deps.Logger},
	}
	for _, dependency := range missing {
		if depcheck.IsNil(dependency.port) {
			return nil, fmt.Errorf("%w: %s", ErrIncompleteDependencies, dependency.name)
		}
	}
	return &Builder{
		goals: deps.Goals, tracking: deps.Tracking,
		projects: deps.Projects, decorations: deps.Decorations, invoices: deps.Invoices, logger: deps.Logger,
	}, nil
}

func (b *Builder) Build(ctx context.Context, query Query) (Snapshot, error) {
	if query.TeamID <= 0 || query.Now.IsZero() {
		return Snapshot{}, ErrInvalidQuery
	}
	now := query.Now
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	todayEnd := now
	todayQueryEnd := todayStart.AddDate(0, 0, 1)
	recentStart := now.AddDate(0, 0, -7)
	recentEnd := now
	snapshot := Snapshot{
		TodayStart: todayStart, TodayEnd: todayEnd,
		RecentStart: recentStart, RecentEnd: recentEnd,
	}
	var err error
	if snapshot.Activities, err = b.goals.Activities(ctx, query.TeamID); err != nil {
		return Snapshot{}, fmt.Errorf("load dashboard activities: %w", err)
	}
	if snapshot.ActiveSessions, err = b.tracking.ActiveSessions(ctx, query.TeamID); err != nil {
		return Snapshot{}, fmt.Errorf("load active dashboard sessions: %w", err)
	}
	if snapshot.TodaySessions, err = b.tracking.ClosedSessions(ctx, appmodel.ClosedSessionsQuery{TeamID: query.TeamID, Start: todayStart, End: todayQueryEnd}); err != nil {
		return Snapshot{}, fmt.Errorf("load today's dashboard sessions: %w", err)
	}
	if snapshot.RecentSessions, err = b.tracking.ClosedSessions(ctx, appmodel.ClosedSessionsQuery{TeamID: query.TeamID, Start: recentStart, End: recentEnd}); err != nil {
		return Snapshot{}, fmt.Errorf("load recent dashboard sessions: %w", err)
	}
	snapshot.TodayTotalSeconds, snapshot.TopActivityName, err = summarizeToday(snapshot.TodaySessions, snapshot.ActiveSessions, todayStart, todayEnd, now)
	if err != nil {
		return Snapshot{}, err
	}
	if snapshot.Projects, err = b.projects.List(ctx, appmodel.ProjectCatalogQuery{TeamID: query.TeamID}); err != nil {
		return Snapshot{}, fmt.Errorf("load dashboard projects: %w", err)
	}
	decorations, err := b.decorations.Build(ctx, appmodel.SessionDecorationRequest{
		TeamID: query.TeamID, Sessions: combineSessions(snapshot.ActiveSessions, snapshot.TodaySessions, snapshot.RecentSessions),
		IncludeTags: true, IncludeProjects: true,
	})
	if err != nil {
		return Snapshot{}, fmt.Errorf("build dashboard session decorations: %w", err)
	}
	snapshot.TagsBySession, snapshot.ProjectsByID = decorations.TagsBySession, decorations.ProjectsByID
	snapshot.HasSession = len(snapshot.ActiveSessions) > 0 || len(snapshot.RecentSessions) > 0
	if !snapshot.HasSession {
		snapshot.HasSession, err = b.tracking.HasAnySession(ctx, query.TeamID)
		if err != nil {
			return Snapshot{}, fmt.Errorf("check dashboard first run: %w", err)
		}
	}
	// Goal progress is an optional dashboard widget; a failed read hides it
	// without making the rest of the page unavailable.
	if snapshot.Goals, err = b.goals.Progress(ctx, appmodel.GoalProgressQuery{TeamID: query.TeamID, Now: now}); err != nil {
		b.logger.Printf("dashboard: load goal progress for team %d: %v", query.TeamID, err)
		snapshot.Goals = nil
	}
	if query.IncludeBilling && len(snapshot.Projects) > 0 {
		snapshot.Unbilled, err = b.invoices.UnbilledProjectTime(ctx, appmodel.UnbilledProjectQuery{TeamID: query.TeamID})
		if err != nil {
			return Snapshot{}, fmt.Errorf("load dashboard unbilled time: %w", err)
		}
	}
	return snapshot, nil
}

func summarizeToday(today, active []model.ActiveSession, from, through, now time.Time) (int, string, error) {
	activityTotals := make(map[string]int)
	total := 0
	for _, group := range [][]model.ActiveSession{today, active} {
		for _, item := range group {
			seconds := item.Session.TrackedSecondsInWindow(from, through, now)
			var err error
			total, err = money.AddInt(total, seconds)
			if err != nil {
				return 0, "", fmt.Errorf("sum dashboard time: %w", err)
			}
			activityTotals[item.Activity.Name], err = money.AddInt(activityTotals[item.Activity.Name], seconds)
			if err != nil {
				return 0, "", fmt.Errorf("sum dashboard activity time: %w", err)
			}
		}
	}
	topName, topSeconds := "", 0
	for name, seconds := range activityTotals {
		if seconds > topSeconds || seconds == topSeconds && seconds > 0 && name < topName {
			topName, topSeconds = name, seconds
		}
	}
	return total, topName, nil
}

// BuildActiveList coordinates the read model used by the active-session
// fragment. Store access remains in the owning workflows; first-run state and
// optional row enrichments are assembled here rather than in the HTTP adapter.
func (b *Builder) BuildActiveList(ctx context.Context, teamID int64) (appmodel.ActiveListSnapshot, error) {
	if teamID <= 0 {
		return appmodel.ActiveListSnapshot{}, ErrInvalidQuery
	}
	active, err := b.tracking.ActiveSessions(ctx, teamID)
	if err != nil {
		return appmodel.ActiveListSnapshot{}, fmt.Errorf("load active sessions: %w", err)
	}
	projects, err := b.projects.List(ctx, appmodel.ProjectCatalogQuery{TeamID: teamID})
	if err != nil {
		return appmodel.ActiveListSnapshot{}, fmt.Errorf("load active-list projects: %w", err)
	}
	decorations, err := b.decorations.Build(ctx, appmodel.SessionDecorationRequest{
		TeamID: teamID, Sessions: active, IncludeTags: true, IncludeProjects: true,
	})
	if err != nil {
		return appmodel.ActiveListSnapshot{}, fmt.Errorf("build active-list session decorations: %w", err)
	}
	firstRun := false
	if len(active) == 0 {
		hasSession, err := b.tracking.HasAnySession(ctx, teamID)
		if err != nil {
			return appmodel.ActiveListSnapshot{}, fmt.Errorf("check active-list first run: %w", err)
		}
		firstRun = !hasSession
	}
	return appmodel.ActiveListSnapshot{
		ActiveSessions: active, Projects: projects, TagsBySession: decorations.TagsBySession,
		ProjectsByID: decorations.ProjectsByID, FirstRun: firstRun,
	}, nil
}

func combineSessions(groups ...[]model.ActiveSession) []model.ActiveSession {
	var sessions []model.ActiveSession
	for _, group := range groups {
		sessions = append(sessions, group...)
	}
	return sessions
}
