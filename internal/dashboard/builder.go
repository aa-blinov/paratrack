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
	Progress(context.Context, int64, time.Time) ([]model.GoalProgress, error)
}

type TrackingReader interface {
	ActiveSessions(context.Context, int64) ([]model.ActiveSession, error)
	ClosedSessions(context.Context, int64, time.Time, time.Time, *int64) ([]model.ActiveSession, error)
	HasAnySession(context.Context, int64) (bool, error)
}

type ProjectReader interface {
	List(context.Context, int64, bool) ([]model.Project, error)
	Summaries(context.Context, int64, []int64) (map[int64]model.ProjectSummary, error)
}

type TagReader interface {
	TagsForSessions(context.Context, int64, []int64) (map[int64][]model.Tag, error)
}

type InvoiceReader interface {
	UnbilledProjectTime(context.Context, int64, int64) ([]model.UnbilledProject, error)
}

type Logger interface {
	Printf(string, ...any)
}

type Dependencies struct {
	Goals    GoalReader
	Tracking TrackingReader
	Projects ProjectReader
	Tags     TagReader
	Invoices InvoiceReader
	Logger   Logger
}

var (
	ErrIncompleteDependencies = errors.New("dashboard builder dependencies are incomplete")
	ErrInvalidQuery           = errors.New("invalid dashboard query")
)

type Builder struct {
	goals    GoalReader
	tracking TrackingReader
	projects ProjectReader
	tags     TagReader
	invoices InvoiceReader
	logger   Logger
}

type Query = appmodel.DashboardQuery
type Snapshot = appmodel.DashboardSnapshot

func NewBuilder(deps Dependencies) (*Builder, error) {
	missing := []struct {
		name string
		port any
	}{
		{"goals", deps.Goals}, {"tracking", deps.Tracking},
		{"projects", deps.Projects}, {"tags", deps.Tags}, {"invoices", deps.Invoices},
		{"logger", deps.Logger},
	}
	for _, dependency := range missing {
		if depcheck.IsNil(dependency.port) {
			return nil, fmt.Errorf("%w: %s", ErrIncompleteDependencies, dependency.name)
		}
	}
	return &Builder{
		goals: deps.Goals, tracking: deps.Tracking,
		projects: deps.Projects, tags: deps.Tags, invoices: deps.Invoices, logger: deps.Logger,
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
	if snapshot.TodaySessions, err = b.tracking.ClosedSessions(ctx, query.TeamID, todayStart, todayQueryEnd, nil); err != nil {
		return Snapshot{}, fmt.Errorf("load today's dashboard sessions: %w", err)
	}
	if snapshot.RecentSessions, err = b.tracking.ClosedSessions(ctx, query.TeamID, recentStart, recentEnd, nil); err != nil {
		return Snapshot{}, fmt.Errorf("load recent dashboard sessions: %w", err)
	}
	snapshot.TodayTotalSeconds, snapshot.TopActivityName, err = summarizeToday(snapshot.TodaySessions, snapshot.ActiveSessions, todayStart, todayEnd, now)
	if err != nil {
		return Snapshot{}, err
	}
	if snapshot.Projects, err = b.projects.List(ctx, query.TeamID, false); err != nil {
		return Snapshot{}, fmt.Errorf("load dashboard projects: %w", err)
	}
	sessionIDs, projectIDs := collectSessionReferences(snapshot.ActiveSessions, snapshot.TodaySessions, snapshot.RecentSessions)
	snapshot.TagsBySession, snapshot.ProjectsByID = b.enrichSessions(ctx, query.TeamID, sessionIDs, projectIDs)
	snapshot.HasSession = len(snapshot.ActiveSessions) > 0 || len(snapshot.RecentSessions) > 0
	if !snapshot.HasSession {
		snapshot.HasSession, err = b.tracking.HasAnySession(ctx, query.TeamID)
		if err != nil {
			return Snapshot{}, fmt.Errorf("check dashboard first run: %w", err)
		}
	}
	// Goal progress is an optional dashboard widget; a failed read hides it
	// without making the rest of the page unavailable.
	if snapshot.Goals, err = b.goals.Progress(ctx, query.TeamID, now); err != nil {
		b.logger.Printf("dashboard: load goal progress for team %d: %v", query.TeamID, err)
		snapshot.Goals = nil
	}
	if query.IncludeBilling && len(snapshot.Projects) > 0 {
		snapshot.Unbilled, err = b.invoices.UnbilledProjectTime(ctx, query.TeamID, 0)
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
	projects, err := b.projects.List(ctx, teamID, false)
	if err != nil {
		return appmodel.ActiveListSnapshot{}, fmt.Errorf("load active-list projects: %w", err)
	}
	ids, projectIDs := collectSessionReferences(active)
	tags, summaries := b.enrichSessions(ctx, teamID, ids, projectIDs)
	firstRun := false
	if len(active) == 0 {
		hasSession, err := b.tracking.HasAnySession(ctx, teamID)
		if err != nil {
			return appmodel.ActiveListSnapshot{}, fmt.Errorf("check active-list first run: %w", err)
		}
		firstRun = !hasSession
	}
	return appmodel.ActiveListSnapshot{
		ActiveSessions: active, Projects: projects, TagsBySession: tags,
		ProjectsByID: summaries, FirstRun: firstRun,
	}, nil
}

func collectSessionReferences(sessionGroups ...[]model.ActiveSession) ([]int64, []int64) {
	var sessionIDs, projectIDs []int64
	seenSessions, seenProjects := map[int64]bool{}, map[int64]bool{}
	for _, sessions := range sessionGroups {
		for _, item := range sessions {
			if !seenSessions[item.Session.ID] {
				seenSessions[item.Session.ID] = true
				sessionIDs = append(sessionIDs, item.Session.ID)
			}
			projectID := item.Activity.ProjectID
			if projectID > 0 && !seenProjects[projectID] {
				seenProjects[projectID] = true
				projectIDs = append(projectIDs, projectID)
			}
		}
	}
	return sessionIDs, projectIDs
}

func (b *Builder) enrichSessions(ctx context.Context, teamID int64, sessionIDs, projectIDs []int64) (map[int64][]model.Tag, map[int64]model.ProjectSummary) {
	var tagsBySession map[int64][]model.Tag
	var projectsByID map[int64]model.ProjectSummary
	// Row decorations are best-effort so a tags or project lookup failure does
	// not make the active timer list unavailable.
	if len(sessionIDs) > 0 {
		tags, err := b.tags.TagsForSessions(ctx, teamID, sessionIDs)
		if err != nil {
			b.logger.Printf("dashboard: load session tags for team %d: %v", teamID, err)
		} else {
			tagsBySession = tags
		}
	}
	if len(projectIDs) > 0 {
		summaries, err := b.projects.Summaries(ctx, teamID, projectIDs)
		if err != nil {
			b.logger.Printf("dashboard: load project summaries for team %d: %v", teamID, err)
		} else {
			projectsByID = summaries
		}
	}
	return tagsBySession, projectsByID
}
