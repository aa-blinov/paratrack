// Package crm owns client relationship input rules: contacts, the deal pipeline
// and the activity timeline. Who may change a deal is decided in the persistence
// transaction, not here.
package crm

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
	CreateCRMContact(context.Context, appmodel.CRMContactCreateRequest) (model.CRMContact, error)
	ListCRMContacts(context.Context, appmodel.CRMContactListQuery) ([]model.CRMContact, error)
	ListCRMStages(context.Context, appmodel.CRMStageListQuery) ([]model.CRMStage, error)
	CreateCRMStage(context.Context, appmodel.CRMStageCreateRequest) (model.CRMStage, error)
	UpdateCRMStage(context.Context, appmodel.CRMStageUpdateRequest) (model.CRMStage, error)
	DeleteCRMStage(context.Context, appmodel.CRMStageDeleteRequest) error
	CreateCRMDeal(context.Context, appmodel.CRMDealCreateRequest) (model.CRMDeal, error)
	GetCRMDeal(context.Context, appmodel.CRMDealLookupQuery) (model.CRMDeal, error)
	ListCRMDeals(context.Context, appmodel.CRMDealListQuery) ([]model.CRMDeal, error)
	UpdateCRMDeal(context.Context, appmodel.CRMDealUpdateRequest) (model.CRMDeal, error)
	MoveCRMDealStage(context.Context, appmodel.CRMDealStageRequest) (model.CRMDeal, error)
	CreateCRMActivity(context.Context, appmodel.CRMActivityCreateRequest) (model.CRMActivity, error)
	ListCRMActivities(context.Context, appmodel.CRMActivityListQuery) ([]model.CRMActivity, error)
}

type Dependencies struct {
	Store Store
}

type Service struct {
	store Store
}

var ErrIncompleteDependencies = errors.New("crm service dependencies are incomplete")

func New(deps Dependencies) (*Service, error) {
	if depcheck.IsNil(deps.Store) {
		return nil, ErrIncompleteDependencies
	}
	return &Service{store: deps.Store}, nil
}

const (
	maxNameLength = 200
	maxNoteLength = 4000
)

func fieldError(field string) error {
	return appmodel.FieldError{Field: field}
}

func validDate(value string) bool {
	_, err := time.Parse(time.DateOnly, value)
	return err == nil
}

func validName(value string) bool {
	return value != "" && len(value) <= maxNameLength
}

func (s *Service) CreateContact(ctx context.Context, request appmodel.CRMContactCreateRequest) (model.CRMContact, error) {
	if request.TeamID <= 0 {
		return model.CRMContact{}, appmodel.ErrInvalidWorkspace
	}
	request.Name = strings.TrimSpace(request.Name)
	if !validName(request.Name) {
		return model.CRMContact{}, fieldError("contact name")
	}
	if request.ClientID <= 0 {
		return model.CRMContact{}, fieldError("client")
	}
	return s.store.CreateCRMContact(ctx, request)
}

func (s *Service) Contacts(ctx context.Context, query appmodel.CRMContactListQuery) ([]model.CRMContact, error) {
	if query.TeamID <= 0 {
		return nil, appmodel.ErrInvalidWorkspace
	}
	return s.store.ListCRMContacts(ctx, query)
}

func (s *Service) CreateDeal(ctx context.Context, request appmodel.CRMDealCreateRequest) (model.CRMDeal, error) {
	if request.TeamID <= 0 {
		return model.CRMDeal{}, appmodel.ErrInvalidWorkspace
	}
	request.Title = strings.TrimSpace(request.Title)
	if !validName(request.Title) {
		return model.CRMDeal{}, fieldError("deal title")
	}
	if err := checkMoney(request.AmountCents, request.Probability); err != nil {
		return model.CRMDeal{}, err
	}
	request.Currency = strings.ToUpper(strings.TrimSpace(request.Currency))
	if request.Currency != "" && len(request.Currency) != 3 {
		return model.CRMDeal{}, fieldError("currency")
	}
	if request.CloseDate != "" && !validDate(request.CloseDate) {
		return model.CRMDeal{}, fieldError("close date")
	}
	if request.NextStepAt != "" && !validDate(request.NextStepAt) {
		return model.CRMDeal{}, fieldError("next step date")
	}
	return s.store.CreateCRMDeal(ctx, request)
}

// checkMoney keeps amounts non-negative and probabilities in percent.
func checkMoney(amountCents int64, probability int) error {
	if amountCents < 0 {
		return fieldError("amount")
	}
	if probability < 0 || probability > 100 {
		return fieldError("probability")
	}
	return nil
}

func (s *Service) Deal(ctx context.Context, query appmodel.CRMDealLookupQuery) (model.CRMDeal, error) {
	if query.TeamID <= 0 {
		return model.CRMDeal{}, appmodel.ErrInvalidWorkspace
	}
	return s.store.GetCRMDeal(ctx, query)
}

// Deals lists the pipeline. An overdue filter needs the workspace day, because
// a deal is overdue when its next step is before that day.
func (s *Service) Deals(ctx context.Context, query appmodel.CRMDealListQuery) ([]model.CRMDeal, error) {
	if query.TeamID <= 0 {
		return nil, appmodel.ErrInvalidWorkspace
	}
	if query.Today != "" && !validDate(query.Today) {
		return nil, fieldError("today")
	}
	if query.Overdue && query.Today == "" {
		return nil, fieldError("today")
	}
	return s.store.ListCRMDeals(ctx, query)
}

// Pipeline counts the deals of each stage, in the workspace's stage order, and the
// open deals that are overdue.
func (s *Service) Pipeline(ctx context.Context, query appmodel.CRMDealListQuery) (appmodel.CRMPipeline, error) {
	if query.TeamID <= 0 {
		return appmodel.CRMPipeline{}, appmodel.ErrInvalidWorkspace
	}
	if query.Today == "" || !validDate(query.Today) {
		return appmodel.CRMPipeline{}, fieldError("today")
	}
	stages, err := s.store.ListCRMStages(ctx, appmodel.CRMStageListQuery{TeamID: query.TeamID, CallerID: query.CallerID})
	if err != nil {
		return appmodel.CRMPipeline{}, err
	}
	deals, err := s.store.ListCRMDeals(ctx, appmodel.CRMDealListQuery{TeamID: query.TeamID, CallerID: query.CallerID, OwnerID: query.OwnerID})
	if err != nil {
		return appmodel.CRMPipeline{}, err
	}
	pipeline := appmodel.CRMPipeline{Stages: make([]appmodel.CRMPipelineStage, len(stages))}
	position := make(map[string]int, len(stages))
	for i, stage := range stages {
		pipeline.Stages[i] = appmodel.CRMPipelineStage{Stage: stage.Key, Name: stage.Name, Kind: stage.Kind}
		position[stage.Key] = i
	}
	for _, deal := range deals {
		if i, ok := position[deal.Stage]; ok {
			pipeline.Stages[i].Count++
		}
		if deal.Overdue(query.Today) {
			pipeline.Overdue++
		}
	}
	return pipeline, nil
}

// Stages lists the workspace's pipeline in order. Every member reads it.
func (s *Service) Stages(ctx context.Context, query appmodel.CRMStageListQuery) ([]model.CRMStage, error) {
	if query.TeamID <= 0 {
		return nil, appmodel.ErrInvalidWorkspace
	}
	return s.store.ListCRMStages(ctx, query)
}

// CreateStage appends a stage. The kind defaults to open. Only a manager may change
// the pipeline; the persistence transaction checks the role again.
func (s *Service) CreateStage(ctx context.Context, request appmodel.CRMStageCreateRequest) (model.CRMStage, error) {
	if request.TeamID <= 0 {
		return model.CRMStage{}, appmodel.ErrInvalidWorkspace
	}
	if !request.Manager {
		return model.CRMStage{}, model.ErrForbidden
	}
	request.Name = strings.TrimSpace(request.Name)
	if !validName(request.Name) {
		return model.CRMStage{}, fieldError("stage name")
	}
	if request.Kind == "" {
		request.Kind = model.StageKindOpen
	}
	if !model.ValidStageKind(request.Kind) {
		return model.CRMStage{}, fieldError("kind")
	}
	return s.store.CreateCRMStage(ctx, request)
}

// UpdateStage renames a stage, sets its kind or moves it; a nil field stays as it is.
func (s *Service) UpdateStage(ctx context.Context, request appmodel.CRMStageUpdateRequest) (model.CRMStage, error) {
	if request.TeamID <= 0 {
		return model.CRMStage{}, appmodel.ErrInvalidWorkspace
	}
	if !request.Manager {
		return model.CRMStage{}, model.ErrForbidden
	}
	if request.Name != nil {
		name := strings.TrimSpace(*request.Name)
		if !validName(name) {
			return model.CRMStage{}, fieldError("stage name")
		}
		request.Name = &name
	}
	if request.Kind != nil && !model.ValidStageKind(*request.Kind) {
		return model.CRMStage{}, fieldError("kind")
	}
	if request.Position != nil && *request.Position < 1 {
		return model.CRMStage{}, fieldError("position")
	}
	return s.store.UpdateCRMStage(ctx, request)
}

// MoveStage puts a stage at a 1-based position.
func (s *Service) MoveStage(ctx context.Context, request appmodel.CRMStageMoveRequest) (model.CRMStage, error) {
	if request.TeamID <= 0 {
		return model.CRMStage{}, appmodel.ErrInvalidWorkspace
	}
	if request.Position < 1 {
		return model.CRMStage{}, fieldError("position")
	}
	position := request.Position
	return s.UpdateStage(ctx, appmodel.CRMStageUpdateRequest{
		TeamID: request.TeamID, CallerID: request.CallerID, Manager: request.Manager, Key: request.Key, Position: &position,
	})
}

// DeleteStage removes a stage that has no deals. A stage with deals is refused with a
// field error on "stage"; its deals are not moved for it.
func (s *Service) DeleteStage(ctx context.Context, request appmodel.CRMStageDeleteRequest) error {
	if request.TeamID <= 0 {
		return appmodel.ErrInvalidWorkspace
	}
	if !request.Manager {
		return model.ErrForbidden
	}
	return s.store.DeleteCRMStage(ctx, request)
}

// UpdateDeal changes the fields that are set; a nil field stays as it is.
func (s *Service) UpdateDeal(ctx context.Context, request appmodel.CRMDealUpdateRequest) (model.CRMDeal, error) {
	if request.TeamID <= 0 {
		return model.CRMDeal{}, appmodel.ErrInvalidWorkspace
	}
	if request.Title != nil {
		title := strings.TrimSpace(*request.Title)
		if !validName(title) {
			return model.CRMDeal{}, fieldError("deal title")
		}
		request.Title = &title
	}
	if request.AmountCents != nil || request.Probability != nil {
		amount, probability := int64(0), 0
		if request.AmountCents != nil {
			amount = *request.AmountCents
		}
		if request.Probability != nil {
			probability = *request.Probability
		}
		if err := checkMoney(amount, probability); err != nil {
			return model.CRMDeal{}, err
		}
	}
	if request.CloseDate != nil && *request.CloseDate != "" && !validDate(*request.CloseDate) {
		return model.CRMDeal{}, fieldError("close date")
	}
	if request.NextStepAt != nil && *request.NextStepAt != "" && !validDate(*request.NextStepAt) {
		return model.CRMDeal{}, fieldError("next step date")
	}
	return s.store.UpdateCRMDeal(ctx, request)
}

func (s *Service) MoveDealStage(ctx context.Context, request appmodel.CRMDealStageRequest) (model.CRMDeal, error) {
	if request.TeamID <= 0 {
		return model.CRMDeal{}, appmodel.ErrInvalidWorkspace
	}
	request.LostReason = strings.TrimSpace(request.LostReason)
	if len(request.LostReason) > maxNameLength {
		return model.CRMDeal{}, fieldError("lost reason")
	}
	return s.store.MoveCRMDealStage(ctx, request)
}

// LogActivity records a call, a meeting or a note on a client or a deal. The
// kind defaults to a note, and an entry needs some text.
func (s *Service) LogActivity(ctx context.Context, request appmodel.CRMActivityCreateRequest) (model.CRMActivity, error) {
	if request.TeamID <= 0 {
		return model.CRMActivity{}, appmodel.ErrInvalidWorkspace
	}
	if request.ClientID <= 0 && request.DealID <= 0 {
		return model.CRMActivity{}, fieldError("client or deal")
	}
	request.Body = strings.TrimSpace(request.Body)
	request.Result = strings.TrimSpace(request.Result)
	if request.Body == "" && request.Result == "" {
		return model.CRMActivity{}, fieldError("body")
	}
	if len(request.Body) > maxNoteLength || len(request.Result) > maxNoteLength {
		return model.CRMActivity{}, fieldError("body")
	}
	if request.Kind == "" {
		request.Kind = "note"
	}
	return s.store.CreateCRMActivity(ctx, request)
}

func (s *Service) Activities(ctx context.Context, query appmodel.CRMActivityListQuery) ([]model.CRMActivity, error) {
	if query.TeamID <= 0 {
		return nil, appmodel.ErrInvalidWorkspace
	}
	if query.ClientID <= 0 && query.DealID <= 0 {
		return nil, fieldError("client or deal")
	}
	return s.store.ListCRMActivities(ctx, query)
}
