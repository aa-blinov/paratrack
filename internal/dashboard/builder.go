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
	if snapshot.Projects, err = b.projects.List(ctx, query.TeamID, false); err != nil {
		return Snapshot{}, fmt.Errorf("load dashboard projects: %w", err)
	}
	sessionIDs := make([]int64, 0, len(snapshot.ActiveSessions)+len(snapshot.TodaySessions)+len(snapshot.RecentSessions))
	projectIDs := make([]int64, 0)
	seenSessions, seenProjects := map[int64]bool{}, map[int64]bool{}
	for _, sessions := range [][]model.ActiveSession{snapshot.ActiveSessions, snapshot.TodaySessions, snapshot.RecentSessions} {
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
	// These are display enrichments. Failed optional lookups should not hide
	// the dashboard, matching the adapter's former best-effort behavior. Log
	// failures so an incomplete snapshot remains diagnosable.
	if len(sessionIDs) > 0 {
		tags, err := b.tags.TagsForSessions(ctx, query.TeamID, sessionIDs)
		if err != nil {
			b.logger.Printf("dashboard: load session tags for team %d: %v", query.TeamID, err)
		} else {
			snapshot.TagsBySession = tags
		}
	}
	if len(projectIDs) > 0 {
		summaries, err := b.projects.Summaries(ctx, query.TeamID, projectIDs)
		if err != nil {
			b.logger.Printf("dashboard: load project summaries for team %d: %v", query.TeamID, err)
		} else {
			snapshot.ProjectsByID = summaries
		}
	}
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
