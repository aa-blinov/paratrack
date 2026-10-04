// Package integrations owns synchronization rules for tasks fetched from
// external providers. Provider HTTP clients and web responses are adapters.
package integrations

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/catalog"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/integrationport"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/postcommit"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

var ErrInvalidTask = appmodel.ErrInvalidExternalTask
var ErrIncompleteSnapshot = appmodel.ErrIncompleteTaskSnapshot
var ErrInvalidIntegration = appmodel.ErrInvalidIntegration

const MaxSyncTasks = integrationport.MaxSyncTasks

// IntegrationManager owns connection creation and removal.
type IntegrationManager interface {
	CreateIntegration(context.Context, appmodel.IntegrationCreateRequest) (model.IntegrationSummary, error)
	DeleteIntegration(context.Context, appmodel.IntegrationMutationRequest) error
}

// IntegrationCatalog provides credential-free workspace summaries.
type IntegrationCatalog interface {
	ListIntegrations(context.Context, int64) ([]model.IntegrationSummary, error)
	GetIntegrationSummary(context.Context, int64, int64) (model.IntegrationSummary, error)
}

// IntegrationTaskStore owns task queries and atomic snapshot synchronization.
type IntegrationTaskStore interface {
	ListExternalTasks(context.Context, int64, int64) ([]model.ExternalTask, error)
	GetExternalTask(context.Context, int64, int64) (model.ExternalTask, error)
	ListExternalTasksForTeam(context.Context, int64) ([]model.ExternalTaskWithProvider, error)
	SyncExternalTasks(context.Context, appmodel.IntegrationTaskSyncRequest) error
}

// IntegrationSyncStarter atomically reserves the newest snapshot generation
// and returns credentials only to the manager-authorized sync path.
type IntegrationSyncStarter interface {
	BeginIntegrationSync(context.Context, appmodel.IntegrationSyncStartRequest) (appmodel.IntegrationSyncCredentials, error)
}

type Dependencies struct {
	Manager     IntegrationManager
	Catalog     IntegrationCatalog
	Tasks       IntegrationTaskStore
	SyncStarter IntegrationSyncStarter
	Audit       AuditRecorder
	Logger      Logger
}

type AuditRecorder interface {
	Record(context.Context, model.AuditRecord) error
}

type Logger interface {
	Printf(string, ...any)
}

type ProviderInput = integrationport.ProviderInput
type ProviderClient = integrationport.ProviderClient

// ConnectResult preserves the created connection even if its initial provider
// sync fails, so transports can render the detail page with a sync error.
type ConnectResult = appmodel.IntegrationConnectResult

type Service struct {
	manager     IntegrationManager
	catalog     IntegrationCatalog
	tasks       IntegrationTaskStore
	syncStarter IntegrationSyncStarter
	providers   ProviderClient
	audit       AuditRecorder
	logger      Logger
}

var ErrIncompleteDependencies = errors.New("integration service dependencies are incomplete")

func New(deps Dependencies, providers ProviderClient) (*Service, error) {
	missing := []struct {
		name string
		port any
	}{
		{"manager", deps.Manager}, {"catalog", deps.Catalog},
		{"tasks", deps.Tasks}, {"integration sync starter", deps.SyncStarter},
		{"audit recorder", deps.Audit}, {"logger", deps.Logger},
		{"provider client", providers},
	}
	for _, dependency := range missing {
		if depcheck.IsNil(dependency.port) {
			return nil, fmt.Errorf("%w: %s", ErrIncompleteDependencies, dependency.name)
		}
	}
	return &Service{
		manager: deps.Manager, catalog: deps.Catalog,
		tasks: deps.Tasks, syncStarter: deps.SyncStarter,
		providers: providers, audit: deps.Audit, logger: deps.Logger,
	}, nil
}

func (s *Service) Create(ctx context.Context, request appmodel.IntegrationCreateRequest) (model.IntegrationSummary, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return model.IntegrationSummary{}, fmt.Errorf("%w: team ID must be positive", ErrInvalidIntegration)
	}
	request.Provider = strings.ToLower(strings.TrimSpace(request.Provider))
	definition, ok := catalog.IntegrationByID(request.Provider)
	if !ok || !definition.Available {
		return model.IntegrationSummary{}, fmt.Errorf("%w: provider is not available", ErrInvalidIntegration)
	}
	request.Name, request.Secret = strings.TrimSpace(request.Name), strings.TrimSpace(request.Secret)
	if request.Name == "" || request.Secret == "" {
		return model.IntegrationSummary{}, fmt.Errorf("%w: name and secret are required", ErrInvalidIntegration)
	}
	integration, err := s.manager.CreateIntegration(ctx, request)
	if err != nil {
		return model.IntegrationSummary{}, fmt.Errorf("create integration: %w", err)
	}
	s.recordAudit(ctx, request.TeamID, request.CallerID, "integration.create", fmt.Sprint(integration.ID), request.Provider+" "+request.Name)
	return integration, nil
}

// ConnectAndSync creates a connection and immediately fetches its first
// provider snapshot. Creation failure is fatal; sync failure is returned in
// the result because the connection remains persisted and manageable.
func (s *Service) ConnectAndSync(ctx context.Context, request appmodel.IntegrationCreateRequest) (ConnectResult, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return ConnectResult{}, ErrInvalidIntegration
	}
	integration, err := s.Create(ctx, request)
	if err != nil {
		return ConnectResult{}, err
	}
	count, syncErr := s.SyncProvider(ctx, appmodel.IntegrationMutationRequest{
		TeamID: request.TeamID, IntegrationID: integration.ID, CallerID: request.CallerID,
	})
	return ConnectResult{Integration: integration, Imported: count, SyncError: syncErr}, nil
}

func (s *Service) List(ctx context.Context, teamID int64) ([]model.IntegrationSummary, error) {
	if teamID <= 0 {
		return nil, ErrInvalidIntegration
	}
	integrations, err := s.catalog.ListIntegrations(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("list integrations: %w", err)
	}
	return integrations, nil
}

// Summary returns display data for one connection without exposing credentials.
func (s *Service) Summary(ctx context.Context, teamID, integrationID int64) (model.IntegrationSummary, error) {
	if teamID <= 0 || integrationID <= 0 {
		return model.IntegrationSummary{}, ErrInvalidIntegration
	}
	item, err := s.catalog.GetIntegrationSummary(ctx, teamID, integrationID)
	if err != nil {
		return model.IntegrationSummary{}, fmt.Errorf("get integration summary: %w", err)
	}
	return item, nil
}

func (s *Service) Delete(ctx context.Context, request appmodel.IntegrationMutationRequest) error {
	if request.TeamID <= 0 {
		return ErrInvalidIntegration
	}
	teamID, integrationID, callerID := request.TeamID, request.IntegrationID, request.CallerID
	if integrationID <= 0 || callerID <= 0 {
		return ErrInvalidIntegration
	}
	if err := s.manager.DeleteIntegration(ctx, request); err != nil {
		return fmt.Errorf("delete integration: %w", err)
	}
	s.recordAudit(ctx, teamID, callerID, "integration.delete", fmt.Sprint(integrationID), "")
	return nil
}

func (s *Service) recordAudit(ctx context.Context, teamID, actorID int64, action, target, meta string) {
	effectCtx, cancel := postcommit.NewContext(ctx)
	defer cancel()
	if err := s.audit.Record(effectCtx, model.AuditRecord{
		TeamID: teamID, UserID: actorID, Action: action, Target: target, Meta: meta, IP: requestctx.ClientIP(ctx),
	}); err != nil {
		s.logger.Printf("integrations: record %s audit for team %d: %v", action, teamID, err)
	}
}

func (s *Service) Tasks(ctx context.Context, teamID, integrationID int64) ([]model.ExternalTask, error) {
	if teamID <= 0 || integrationID <= 0 {
		return nil, ErrInvalidIntegration
	}
	tasks, err := s.tasks.ListExternalTasks(ctx, teamID, integrationID)
	if err != nil {
		return nil, fmt.Errorf("list integration tasks: %w", err)
	}
	return tasks, nil
}

func (s *Service) Task(ctx context.Context, teamID, taskID int64) (model.ExternalTask, error) {
	if teamID <= 0 || taskID <= 0 {
		return model.ExternalTask{}, ErrInvalidIntegration
	}
	task, err := s.tasks.GetExternalTask(ctx, teamID, taskID)
	if err != nil {
		return model.ExternalTask{}, fmt.Errorf("get integration task: %w", err)
	}
	return task, nil
}

func (s *Service) TasksForTeam(ctx context.Context, teamID int64) ([]model.ExternalTaskWithProvider, error) {
	if teamID <= 0 {
		return nil, ErrInvalidIntegration
	}
	tasks, err := s.tasks.ListExternalTasksForTeam(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("list team integration tasks: %w", err)
	}
	return tasks, nil
}

// Sync validates provider output before asking the store to replace the
// integration's open-task snapshot. Invalid output must never close local
// tasks that could not be represented by a valid external ID.
func (s *Service) syncTasks(ctx context.Context, teamID, integrationID, callerID int64, tasks []integrationport.ProviderTask) (int, error) {
	return s.syncTasksGeneration(ctx, teamID, integrationID, callerID, 0, tasks)
}

func (s *Service) syncTasksGeneration(ctx context.Context, teamID, integrationID, callerID, generation int64, tasks []integrationport.ProviderTask) (int, error) {
	if teamID <= 0 || integrationID <= 0 || callerID <= 0 {
		return 0, fmt.Errorf("%w: integration ID must be positive", ErrInvalidTask)
	}
	if len(tasks) >= MaxSyncTasks {
		return 0, fmt.Errorf("%w (%d); narrow the integration scope", ErrIncompleteSnapshot, MaxSyncTasks)
	}
	clean := make([]integrationport.ProviderTask, len(tasks))
	copy(clean, tasks)
	for i := range clean {
		clean[i].ExternalID = strings.TrimSpace(clean[i].ExternalID)
		clean[i].Title = strings.TrimSpace(clean[i].Title)
		clean[i].Status = strings.TrimSpace(clean[i].Status)
		if clean[i].ExternalID == "" || clean[i].Title == "" || clean[i].Status == "" {
			return 0, fmt.Errorf("%w at index %d", ErrInvalidTask, i)
		}
	}
	if err := s.tasks.SyncExternalTasks(ctx, appmodel.IntegrationTaskSyncRequest{
		TeamID: teamID, IntegrationID: integrationID, CallerID: callerID, Generation: generation, Tasks: clean,
	}); err != nil {
		return 0, fmt.Errorf("sync integration %d tasks: %w", integrationID, err)
	}
	return len(clean), nil
}

// SyncProvider loads credentials through the workspace-scoped store, fetches
// a fresh provider snapshot and applies it using the same validation path as
// direct synchronization.
func (s *Service) SyncProvider(ctx context.Context, request appmodel.IntegrationMutationRequest) (int, error) {
	if request.TeamID <= 0 {
		return 0, ErrInvalidIntegration
	}
	teamID, integrationID, callerID := request.TeamID, request.IntegrationID, request.CallerID
	if integrationID <= 0 || callerID <= 0 {
		return 0, ErrInvalidIntegration
	}
	if depcheck.IsNil(s.providers) {
		return 0, fmt.Errorf("integration provider client is not configured")
	}
	integration, err := s.syncStarter.BeginIntegrationSync(ctx, appmodel.IntegrationSyncStartRequest(request))
	if err != nil {
		return 0, fmt.Errorf("get integration for sync: %w", err)
	}
	tasks, err := s.providers.FetchTasks(ctx, ProviderInput{
		Provider: integration.Provider,
		Secret:   integration.Secret,
		Config:   integration.Config,
	})
	if err != nil {
		return 0, fmt.Errorf("fetch %s tasks: %w", integration.Provider, err)
	}
	return s.syncTasksGeneration(ctx, teamID, integration.ID, callerID, integration.Generation, tasks)
}
