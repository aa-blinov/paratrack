// Package tagging owns tag input rules and tag-to-session workflows shared
// by the HTTP and CLI adapters. Persistence keeps each association atomic.
package tagging

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
)

// TagStore provides tag lifecycle and tag-to-session association operations.
type TagStore interface {
	CreateTagForMember(context.Context, appmodel.TagCreateRequest) (model.Tag, error)
	DeleteTagForManager(context.Context, appmodel.TagDeleteRequest) error
	AttachTagForMember(context.Context, appmodel.SessionTagRequest) error
	DetachTagForMember(context.Context, appmodel.SessionTagRequest) error
	ListTags(context.Context, appmodel.TagListQuery) ([]model.Tag, error)
	ListAllTagsWithCounts(context.Context, appmodel.TagListQuery) ([]appmodel.TagWithCount, error)
	TagsForSessions(context.Context, appmodel.SessionTagsQuery) (map[int64][]model.Tag, error)
}

// SessionActivityReader provides the scoped lookup needed to refresh a tagged row.
type SessionActivityReader interface {
	GetSession(context.Context, appmodel.SessionLookupQuery) (model.Session, error)
	GetActivity(context.Context, appmodel.ActivityLookupQuery) (model.Activity, error)
}

type Dependencies struct {
	Tags              TagStore
	SessionActivities SessionActivityReader
}

var ErrIncompleteDependencies = errors.New("tagging service dependencies are incomplete")

type Service struct {
	tags              TagStore
	sessionActivities SessionActivityReader
}

func New(deps Dependencies) (*Service, error) {
	if depcheck.IsNil(deps.Tags) {
		return nil, fmt.Errorf("%w: tags", ErrIncompleteDependencies)
	}
	if depcheck.IsNil(deps.SessionActivities) {
		return nil, fmt.Errorf("%w: session activity lookup", ErrIncompleteDependencies)
	}
	return &Service{tags: deps.Tags, sessionActivities: deps.SessionActivities}, nil
}

var (
	ErrTagNotFound    = appmodel.ErrTagNotFound
	ErrInvalidTeam    = appmodel.ErrInvalidTagTeam
	ErrInvalidTag     = appmodel.ErrInvalidTag
	ErrInvalidTagID   = errors.New("tag id must be positive")
	ErrInvalidSession = errors.New("session id must be positive")
)

// CreateForMember creates or resolves a workspace tag after persistence
// rechecks the authenticated member under the workspace lock.
func (s *Service) CreateForMember(ctx context.Context, request appmodel.TagCreateRequest) (model.Tag, error) {
	if request.TeamID <= 0 {
		return model.Tag{}, ErrInvalidTeam
	}
	name, err := normalize(request.TeamID, request.Name)
	if err != nil {
		return model.Tag{}, err
	}
	if request.CallerID <= 0 {
		return model.Tag{}, ErrInvalidTeam
	}
	request.Name = name
	tag, err := s.tags.CreateTagForMember(ctx, request)
	if err != nil {
		return model.Tag{}, fmt.Errorf("create tag: %w", err)
	}
	return tag, nil
}

func (s *Service) List(ctx context.Context, query appmodel.TagListQuery) ([]model.Tag, error) {
	if query.TeamID <= 0 {
		return nil, ErrInvalidTeam
	}
	tags, err := s.tags.ListTags(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	return tags, nil
}

func (s *Service) ListWithCounts(ctx context.Context, query appmodel.TagListQuery) ([]appmodel.TagWithCount, error) {
	if query.TeamID <= 0 {
		return nil, ErrInvalidTeam
	}
	tags, err := s.tags.ListAllTagsWithCounts(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list tags with usage counts: %w", err)
	}
	return tags, nil
}

func (s *Service) TagsForSessions(ctx context.Context, query appmodel.SessionTagsQuery) (map[int64][]model.Tag, error) {
	if query.TeamID <= 0 {
		return nil, ErrInvalidTeam
	}
	for _, id := range query.SessionIDs {
		if id <= 0 {
			return nil, ErrInvalidSession
		}
	}
	tags, err := s.tags.TagsForSessions(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("load tags for sessions: %w", err)
	}
	return tags, nil
}

// SessionActivity returns the session and its activity only when both belong
// to the requested workspace. This is used when rebuilding a tagged row.
func (s *Service) SessionActivity(ctx context.Context, query appmodel.SessionLookupQuery) (model.Session, model.Activity, error) {
	if query.TeamID <= 0 || query.SessionID <= 0 {
		return model.Session{}, model.Activity{}, ErrInvalidSession
	}
	session, err := s.sessionActivities.GetSession(ctx, query)
	if err != nil {
		return model.Session{}, model.Activity{}, fmt.Errorf("load tagged session: %w", err)
	}
	activity, err := s.sessionActivities.GetActivity(ctx, appmodel.ActivityLookupQuery{TeamID: query.TeamID, ActivityID: session.ActivityID})
	if err != nil {
		return model.Session{}, model.Activity{}, fmt.Errorf("load tagged session activity: %w", err)
	}
	return session, activity, nil
}

func (s *Service) Delete(ctx context.Context, request appmodel.TagDeleteRequest) error {
	if request.TeamID <= 0 {
		return ErrInvalidTeam
	}
	if request.CallerID <= 0 {
		return ErrInvalidTeam
	}
	if request.TagID <= 0 {
		return ErrInvalidTagID
	}
	if err := s.tags.DeleteTagForManager(ctx, request); err != nil {
		return fmt.Errorf("delete tag: %w", err)
	}
	return nil
}

// AttachForMember is the authenticated workspace path. Persistence rechecks
// membership under the same workspace lock used by member removal.
func (s *Service) AttachForMember(ctx context.Context, request appmodel.SessionTagRequest) error {
	if request.TeamID <= 0 {
		return ErrInvalidTeam
	}
	name, err := normalize(request.TeamID, request.Name)
	if err != nil {
		return err
	}
	if request.CallerID <= 0 {
		return ErrInvalidTeam
	}
	if request.SessionID <= 0 {
		return ErrInvalidSession
	}
	request.Name = name
	if err := s.tags.AttachTagForMember(ctx, request); err != nil {
		return fmt.Errorf("attach tag: %w", err)
	}
	return nil
}

// DetachForMember is the authenticated workspace path for removing a tag from
// a session.
func (s *Service) DetachForMember(ctx context.Context, request appmodel.SessionTagRequest) error {
	if request.TeamID <= 0 {
		return ErrInvalidTeam
	}
	name, err := normalize(request.TeamID, request.Name)
	if err != nil {
		return err
	}
	if request.CallerID <= 0 {
		return ErrInvalidTeam
	}
	if request.SessionID <= 0 {
		return ErrInvalidSession
	}
	request.Name = name
	if err := s.tags.DetachTagForMember(ctx, request); err != nil {
		return fmt.Errorf("detach tag: %w", err)
	}
	return nil
}

func normalize(teamID int64, name string) (string, error) {
	if teamID <= 0 {
		return "", ErrInvalidTeam
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "", ErrInvalidTag
	}
	return name, nil
}
