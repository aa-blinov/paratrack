package reports

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/reportstats"
)

var ErrInvalidReportQuery = errors.New("invalid report query")
var ErrIncompleteBuilderDependencies = errors.New("report builder dependencies are incomplete")

type SessionReader interface {
	ClosedSessions(context.Context, int64, time.Time, time.Time, *int64) ([]model.ActiveSession, error)
	ClosedSessionsForProject(context.Context, int64, time.Time, time.Time, int64) ([]model.ActiveSession, error)
}

type TagReader interface {
	List(context.Context, int64) ([]model.Tag, error)
	TagsForSessions(context.Context, int64, []int64) (map[int64][]model.Tag, error)
}

type TeamReader interface {
	Members(context.Context, int64) ([]model.TeamMember, error)
	Currency(context.Context, int64) (string, error)
}

type ProjectReader interface {
	List(context.Context, int64, bool) ([]model.Project, error)
	GetBySlug(context.Context, int64, string) (model.Project, error)
	Currencies(context.Context, int64) (map[int64]string, error)
	Summaries(context.Context, int64, []int64) (map[int64]model.ProjectSummary, error)
}

type UserReader interface {
	FindIdentitiesByID(context.Context, []int64) (map[int64]appmodel.UserIdentity, error)
}

type BuilderDependencies struct {
	Sessions SessionReader
	Teams    TeamReader
	Projects ProjectReader
	Users    UserReader
	Tags     TagReader
}

type ReportLabels = appmodel.ReportLabels
type BuildQuery = appmodel.ReportBuildQuery

// Builder coordinates report reads and applies the shared aggregation rules.
type Builder struct {
	sessions SessionReader
	teams    TeamReader
	projects ProjectReader
	users    UserReader
	tags     TagReader
}

func NewBuilder(deps BuilderDependencies) (*Builder, error) {
	missing := []struct {
		name string
		port any
	}{
		{"sessions", deps.Sessions}, {"teams", deps.Teams},
		{"projects", deps.Projects}, {"users", deps.Users}, {"tags", deps.Tags},
	}
	for _, dependency := range missing {
		if depcheck.IsNil(dependency.port) {
			return nil, fmt.Errorf("%w: %s", ErrIncompleteBuilderDependencies, dependency.name)
		}
	}
	return &Builder{
		sessions: deps.Sessions, teams: deps.Teams,
		projects: deps.Projects, users: deps.Users, tags: deps.Tags,
	}, nil
}

type StatsQuery = appmodel.ReportStatsQuery
type StatsResult = appmodel.ReportStatsResult
type GraphQuery = appmodel.ReportGraphQuery
type GraphResult = appmodel.ReportGraphResult

// BuildGraphSessions applies report filters before the HTTP adapter builds its chart view.
func (b *Builder) BuildGraph(ctx context.Context, query GraphQuery) (GraphResult, error) {
	if query.TeamID <= 0 || query.From.IsZero() || !query.To.After(query.From) || query.Now.IsZero() {
		return GraphResult{}, ErrInvalidReportQuery
	}
	project, sessions, err := b.loadProjectSessions(ctx, query.TeamID, query.From, query.To, query.ProjectSlug)
	if err != nil {
		return GraphResult{}, fmt.Errorf("load graph sessions: %w", err)
	}
	filtered := sessions
	if query.Tag != "" {
		ids := make([]int64, 0, len(sessions))
		for _, session := range sessions {
			ids = append(ids, session.Session.ID)
		}
		tagsBySession, err := b.tags.TagsForSessions(ctx, query.TeamID, ids)
		if err != nil {
			return GraphResult{}, fmt.Errorf("load graph session tags: %w", err)
		}
		filtered = make([]model.ActiveSession, 0, len(sessions))
		for _, session := range sessions {
			for _, tag := range tagsBySession[session.Session.ID] {
				if tag.Name == query.Tag {
					filtered = append(filtered, session)
					break
				}
			}
		}
	}
	graph, err := reportstats.HourlyGraph(filtered, query.From, query.To, query.Now)
	if err != nil {
		return GraphResult{}, fmt.Errorf("aggregate hourly graph: %w", err)
	}
	return GraphResult{Project: project, Graph: graph}, nil
}

// BuildStats loads and filters the stats read model before calculating totals.
// Returned sessions have positive tracked time in the requested window.
func (b *Builder) BuildStats(ctx context.Context, query StatsQuery) (StatsResult, error) {
	if query.TeamID <= 0 || query.From.IsZero() || !query.To.After(query.From) || query.Now.IsZero() {
		return StatsResult{}, ErrInvalidReportQuery
	}
	project, sessions, err := b.loadProjectSessions(ctx, query.TeamID, query.From, query.To, query.ProjectSlug)
	if err != nil {
		return StatsResult{}, fmt.Errorf("load stats sessions: %w", err)
	}
	projects, err := b.projects.List(ctx, query.TeamID, false)
	if err != nil {
		return StatsResult{}, fmt.Errorf("load stats projects: %w", err)
	}
	projectByID := make(map[int64]model.Project, len(projects))
	for _, project := range projects {
		projectByID[project.ID] = project
	}
	tags, err := b.tags.List(ctx, query.TeamID)
	if err != nil {
		return StatsResult{}, fmt.Errorf("load stats tags: %w", err)
	}
	ids := make([]int64, 0, len(sessions))
	visible := sessions[:0]
	for _, session := range sessions {
		if session.Session.TrackedSecondsInWindow(query.From, query.To, query.Now) <= 0 {
			continue
		}
		visible = append(visible, session)
		ids = append(ids, session.Session.ID)
	}
	sessions = visible
	tagsBySession, err := b.tags.TagsForSessions(ctx, query.TeamID, ids)
	if err != nil {
		return StatsResult{}, fmt.Errorf("load stats session tags: %w", err)
	}
	if query.Tag != "" {
		filtered := sessions[:0]
		for _, session := range sessions {
			for _, tag := range tagsBySession[session.Session.ID] {
				if tag.Name == query.Tag {
					filtered = append(filtered, session)
					break
				}
			}
		}
		sessions = filtered
	}
	summary, err := SummarizeStats(sessions, projectByID, query.From, query.To, query.Now, query.Uncategorized)
	if err != nil {
		return StatsResult{}, fmt.Errorf("summarize stats: %w", err)
	}
	return StatsResult{Sessions: sessions, TagsBySession: tagsBySession, Projects: projects, Project: project, Tags: tags,
		Summary: summary}, nil
}

// loadProjectSessions resolves an optional slug and applies its workspace
// scope before either stats or graph presentation adds its own filters.
func (b *Builder) loadProjectSessions(ctx context.Context, teamID int64, from, to time.Time, slug string) (model.Project, []model.ActiveSession, error) {
	project, err := b.resolveProjectSlug(ctx, teamID, slug)
	if err != nil {
		return model.Project{}, nil, err
	}
	var sessions []model.ActiveSession
	if project.ID > 0 {
		sessions, err = b.sessions.ClosedSessionsForProject(ctx, teamID, from, to, project.ID)
	} else {
		sessions, err = b.sessions.ClosedSessions(ctx, teamID, from, to, nil)
	}
	return project, sessions, err
}

func (b *Builder) resolveProjectSlug(ctx context.Context, teamID int64, slug string) (model.Project, error) {
	if slug == "" {
		return model.Project{}, nil
	}
	project, err := b.projects.GetBySlug(ctx, teamID, slug)
	if errors.Is(err, model.ErrNotFound) {
		return model.Project{}, nil
	}
	if err != nil {
		return model.Project{}, fmt.Errorf("resolve report project: %w", err)
	}
	return project, nil
}

func (b *Builder) Build(ctx context.Context, query BuildQuery) (AggregateResult, error) {
	if query.TeamID <= 0 || query.From.IsZero() || !query.To.After(query.From) || query.Now.IsZero() {
		return AggregateResult{}, ErrInvalidReportQuery
	}
	sessions, err := b.sessions.ClosedSessions(ctx, query.TeamID, query.From, query.To, nil)
	if err != nil {
		return AggregateResult{}, fmt.Errorf("load report sessions: %w", err)
	}
	teamCurrency, err := b.teams.Currency(ctx, query.TeamID)
	if err != nil {
		return AggregateResult{}, fmt.Errorf("load report currency: %w", err)
	}
	projects, err := b.projects.List(ctx, query.TeamID, true)
	if err != nil {
		return AggregateResult{}, fmt.Errorf("load report projects: %w", err)
	}
	projectByID := make(map[int64]model.Project, len(projects))
	for _, project := range projects {
		projectByID[project.ID] = project
	}
	projectCurrencies, err := b.projects.Currencies(ctx, query.TeamID)
	if err != nil {
		return AggregateResult{}, fmt.Errorf("load report project currencies: %w", err)
	}
	userNames := make(map[int64]string)
	if query.GroupBy == "user" {
		members, err := b.teams.Members(ctx, query.TeamID)
		if err != nil {
			return AggregateResult{}, fmt.Errorf("load report members: %w", err)
		}
		for _, member := range members {
			userNames[member.UserID] = member.Name
			if member.Name == "" {
				userNames[member.UserID] = member.Email
			}
		}
		var formerIDs []int64
		for _, session := range sessions {
			userID := session.Session.UserID
			if userID <= 0 {
				continue
			}
			if _, ok := userNames[userID]; ok {
				continue
			}
			userNames[userID] = query.Labels.FormerMember
			formerIDs = append(formerIDs, userID)
		}
		formerUsers, err := b.users.FindIdentitiesByID(ctx, formerIDs)
		if err != nil {
			return AggregateResult{}, fmt.Errorf("load former report members: %w", err)
		}
		for userID, user := range formerUsers {
			userNames[userID] = user.Name + " (" + query.Labels.FormerMember + ")"
		}
	}
	result, err := Aggregate(AggregateInput{
		Sessions: sessions, Projects: projectByID, ProjectCurrencies: projectCurrencies,
		UserNames: userNames, TeamCurrency: teamCurrency,
		Uncategorized: query.Labels.Uncategorized, UnassignedUser: query.Labels.Unassigned,
		GroupBy: query.GroupBy, Billable: query.Billable,
		From: query.From, To: query.To, Now: query.Now, Location: query.Location,
	})
	if err != nil {
		return AggregateResult{}, fmt.Errorf("aggregate report: %w", err)
	}
	return result, nil
}

// BuildExport loads, decorates and filters the closed session rows used by
// CSV export. Project names are fetched in one scoped batch for all sessions.
func (b *Builder) BuildExport(ctx context.Context, query appmodel.ExportBuildQuery) (appmodel.ExportSnapshot, error) {
	if query.TeamID <= 0 || query.Start.IsZero() || !query.End.After(query.Start) || query.Now.IsZero() {
		return appmodel.ExportSnapshot{}, ErrInvalidReportQuery
	}
	sessions, err := b.sessions.ClosedSessions(ctx, query.TeamID, query.Start, query.End, nil)
	if err != nil {
		return appmodel.ExportSnapshot{}, fmt.Errorf("load export sessions: %w", err)
	}
	visible := make([]model.ActiveSession, 0, len(sessions))
	projectIDs := make([]int64, 0)
	seenProjects := make(map[int64]struct{})
	for _, session := range sessions {
		if (query.HasFrom && session.Session.StartAt.Before(query.Start)) ||
			(query.HasTo && !session.Session.StartAt.Before(query.End)) {
			continue
		}
		visible = append(visible, session)
		projectID := session.Activity.ProjectID
		if projectID <= 0 {
			continue
		}
		if _, seen := seenProjects[projectID]; seen {
			continue
		}
		seenProjects[projectID] = struct{}{}
		projectIDs = append(projectIDs, projectID)
	}
	projectNames := make(map[int64]model.ProjectSummary)
	if len(projectIDs) > 0 {
		projectNames, err = b.projects.Summaries(ctx, query.TeamID, projectIDs)
		if err != nil {
			return appmodel.ExportSnapshot{}, fmt.Errorf("load export project names: %w", err)
		}
	}
	rows := make([]appmodel.ExportRow, 0, len(visible))
	for _, session := range visible {
		row := appmodel.ExportRow{
			SessionID: session.Session.ID, ActivityName: session.Activity.Name,
			ProjectName: projectNames[session.Activity.ProjectID].Name,
			StartAt:     session.Session.StartAt,
		}
		if session.Session.EndAt != nil {
			end := *session.Session.EndAt
			row.EndAt = &end
			row.DurationSeconds = session.Session.DurationSeconds(query.Now)
		}
		if session.Session.Note != nil {
			row.Note = *session.Session.Note
		}
		rows = append(rows, row)
	}
	return appmodel.ExportSnapshot{Rows: rows}, nil
}
