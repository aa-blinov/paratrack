// Package sessiondecorations batches optional tag and project metadata used
// by session row read models.
package sessiondecorations

import (
	"context"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
)

type TagReader interface {
	TagsForSessions(context.Context, appmodel.SessionTagsQuery) (map[int64][]model.Tag, error)
}

type ProjectReader interface {
	Summaries(context.Context, appmodel.ProjectSummariesQuery) (map[int64]model.ProjectSummary, error)
}

type SessionReader interface {
	SessionActivity(context.Context, int64, int64) (model.Session, model.Activity, error)
}

type Logger interface {
	Printf(string, ...any)
}

type Dependencies struct {
	Sessions SessionReader
	Tags     TagReader
	Projects ProjectReader
	Logger   Logger
}

var ErrIncompleteDependencies = errors.New("session decoration builder dependencies are incomplete")
var ErrInvalidRequest = errors.New("invalid session decoration request")

type Builder struct {
	sessions SessionReader
	tags     TagReader
	projects ProjectReader
	logger   Logger
}

func New(deps Dependencies) (*Builder, error) {
	for _, dependency := range []struct {
		name string
		port any
	}{
		{"session reader", deps.Sessions}, {"tag reader", deps.Tags},
		{"project reader", deps.Projects}, {"logger", deps.Logger},
	} {
		if depcheck.IsNil(dependency.port) {
			return nil, fmt.Errorf("%w: %s", ErrIncompleteDependencies, dependency.name)
		}
	}
	return &Builder{sessions: deps.Sessions, tags: deps.Tags, projects: deps.Projects, logger: deps.Logger}, nil
}

func (b *Builder) BuildRow(ctx context.Context, teamID, sessionID int64) (appmodel.SessionDecorationRowSnapshot, error) {
	if teamID <= 0 || sessionID <= 0 {
		return appmodel.SessionDecorationRowSnapshot{}, ErrInvalidRequest
	}
	session, activity, err := b.sessions.SessionActivity(ctx, teamID, sessionID)
	if err != nil {
		return appmodel.SessionDecorationRowSnapshot{}, fmt.Errorf("load session row %d: %w", sessionID, err)
	}
	active := model.ActiveSession{Session: session, Activity: activity}
	decorations, err := b.Build(ctx, appmodel.SessionDecorationRequest{
		TeamID: teamID, Sessions: []model.ActiveSession{active}, IncludeTags: true, IncludeProjects: true,
	})
	if err != nil {
		return appmodel.SessionDecorationRowSnapshot{}, err
	}
	return appmodel.SessionDecorationRowSnapshot{Session: active, Decorations: decorations}, nil
}

func (b *Builder) Build(ctx context.Context, request appmodel.SessionDecorationRequest) (appmodel.SessionDecorationSnapshot, error) {
	if request.TeamID <= 0 {
		return appmodel.SessionDecorationSnapshot{}, ErrInvalidRequest
	}
	snapshot := appmodel.SessionDecorationSnapshot{}
	if len(request.Sessions) == 0 {
		return snapshot, nil
	}
	sessionIDs, projectIDs := collectReferences(request.Sessions)
	if request.IncludeTags && len(sessionIDs) > 0 {
		tags, err := b.tags.TagsForSessions(ctx, appmodel.SessionTagsQuery{TeamID: request.TeamID, SessionIDs: sessionIDs})
		if err != nil {
			b.logger.Printf("sessiondecorations: load tags for team %d: %v", request.TeamID, err)
		} else {
			snapshot.TagsBySession = tags
		}
	}
	if request.IncludeProjects && len(projectIDs) > 0 {
		projects, err := b.projects.Summaries(ctx, appmodel.ProjectSummariesQuery{TeamID: request.TeamID, ProjectIDs: projectIDs})
		if err != nil {
			b.logger.Printf("sessiondecorations: load project summaries for team %d: %v", request.TeamID, err)
		} else {
			snapshot.ProjectsByID = projects
		}
	}
	return snapshot, nil
}

func collectReferences(sessions []model.ActiveSession) ([]int64, []int64) {
	seenSessions, seenProjects := make(map[int64]struct{}, len(sessions)), make(map[int64]struct{}, len(sessions))
	sessionIDs, projectIDs := make([]int64, 0, len(sessions)), make([]int64, 0, len(sessions))
	for _, session := range sessions {
		if id := session.Session.ID; id > 0 {
			if _, exists := seenSessions[id]; !exists {
				seenSessions[id] = struct{}{}
				sessionIDs = append(sessionIDs, id)
			}
		}
		if id := session.Activity.ProjectID; id > 0 {
			if _, exists := seenProjects[id]; !exists {
				seenProjects[id] = struct{}{}
				projectIDs = append(projectIDs, id)
			}
		}
	}
	return sessionIDs, projectIDs
}
