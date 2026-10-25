// Package calendar owns team event input rules. Who sees and changes an event
// (its author, its participants, the managers, the whole team when shared) is
// decided in the persistence transaction, not here.
package calendar

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
)

// Store is the persistence contract, satisfied by the database adapter.
type Store interface {
	CreateCalendarEvent(context.Context, appmodel.CalendarEventCreateRequest) (model.CalendarEvent, error)
	UpdateCalendarEvent(context.Context, appmodel.CalendarEventUpdateRequest) (model.CalendarEvent, error)
	DeleteCalendarEvent(context.Context, appmodel.CalendarEventDeleteRequest) error
	ListCalendarEvents(context.Context, appmodel.CalendarEventListQuery) ([]model.CalendarEvent, error)
}

type Dependencies struct {
	Store Store
}

type Service struct {
	store Store
}

var ErrIncompleteDependencies = errors.New("calendar service dependencies are incomplete")

func New(deps Dependencies) (*Service, error) {
	if depcheck.IsNil(deps.Store) {
		return nil, ErrIncompleteDependencies
	}
	return &Service{store: deps.Store}, nil
}

const (
	maxTitleLength    = 200
	maxLocationLength = 300
	maxNotesLength    = 4000
	// maxRange keeps one agenda read to a year, so a call cannot ask for the
	// whole history of a team.
	maxRange = 366 * 24 * time.Hour
)

func fieldError(field string) error {
	return appmodel.FieldError{Field: field}
}

func (s *Service) Create(ctx context.Context, request appmodel.CalendarEventCreateRequest) (model.CalendarEvent, error) {
	if request.TeamID <= 0 {
		return model.CalendarEvent{}, appmodel.ErrInvalidWorkspace
	}
	request.Title = strings.TrimSpace(request.Title)
	if request.Title == "" || len(request.Title) > maxTitleLength {
		return model.CalendarEvent{}, fieldError("event title")
	}
	if request.StartsAt.IsZero() || request.EndsAt.IsZero() {
		return model.CalendarEvent{}, fieldError("start and end")
	}
	request.Location = strings.TrimSpace(request.Location)
	request.Notes = strings.TrimSpace(request.Notes)
	if len(request.Location) > maxLocationLength || len(request.Notes) > maxNotesLength {
		return model.CalendarEvent{}, fieldError("location or notes")
	}
	return s.store.CreateCalendarEvent(ctx, request)
}

// Update changes the fields that are set. Participants replace the list only
// when ParticipantIDs is not nil.
func (s *Service) Update(ctx context.Context, request appmodel.CalendarEventUpdateRequest) (model.CalendarEvent, error) {
	if request.TeamID <= 0 {
		return model.CalendarEvent{}, appmodel.ErrInvalidWorkspace
	}
	if request.Title != nil {
		title := strings.TrimSpace(*request.Title)
		if title == "" || len(title) > maxTitleLength {
			return model.CalendarEvent{}, fieldError("event title")
		}
		request.Title = &title
	}
	if (request.StartsAt != nil && request.StartsAt.IsZero()) || (request.EndsAt != nil && request.EndsAt.IsZero()) {
		return model.CalendarEvent{}, fieldError("start and end")
	}
	if request.Location != nil {
		location := strings.TrimSpace(*request.Location)
		if len(location) > maxLocationLength {
			return model.CalendarEvent{}, fieldError("location")
		}
		request.Location = &location
	}
	if request.Notes != nil {
		notes := strings.TrimSpace(*request.Notes)
		if len(notes) > maxNotesLength {
			return model.CalendarEvent{}, fieldError("notes")
		}
		request.Notes = &notes
	}
	return s.store.UpdateCalendarEvent(ctx, request)
}

func (s *Service) Delete(ctx context.Context, request appmodel.CalendarEventDeleteRequest) error {
	if request.TeamID <= 0 {
		return appmodel.ErrInvalidWorkspace
	}
	return s.store.DeleteCalendarEvent(ctx, request)
}

// Agenda lists the events that overlap the window [From, To). The window is an
// instant range; the caller converts the workspace day to it.
func (s *Service) Agenda(ctx context.Context, query appmodel.CalendarEventListQuery) ([]model.CalendarEvent, error) {
	if query.TeamID <= 0 {
		return nil, appmodel.ErrInvalidWorkspace
	}
	if query.From.IsZero() || !query.To.After(query.From) || query.To.Sub(query.From) > maxRange {
		return nil, fieldError("from and to")
	}
	return s.store.ListCalendarEvents(ctx, query)
}
