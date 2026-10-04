package integrations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/integrationport"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

type integrationStoreStub struct {
	IntegrationCatalog
	IntegrationTaskStore
	IntegrationSyncStarter
	created     model.IntegrationSummary
	credential  appmodel.IntegrationSyncCredentials
	deleteErr   error
	syncCalls   int
	syncRequest appmodel.IntegrationTaskSyncRequest
}

func (s *integrationStoreStub) CreateIntegration(_ context.Context, request appmodel.IntegrationCreateRequest) (model.IntegrationSummary, error) {
	s.created = model.IntegrationSummary{ID: 17, TeamID: request.TeamID, Provider: request.Provider, Name: request.Name, CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}
	return s.created, nil
}

func (s *integrationStoreStub) DeleteIntegration(context.Context, appmodel.IntegrationMutationRequest) error {
	return s.deleteErr
}

func (s *integrationStoreStub) BeginIntegrationSync(context.Context, appmodel.IntegrationSyncStartRequest) (appmodel.IntegrationSyncCredentials, error) {
	if s.credential.ID == 0 {
		s.credential = appmodel.IntegrationSyncCredentials{ID: s.created.ID, TeamID: s.created.TeamID, Provider: s.created.Provider}
		s.credential.Secret = "stored-secret"
		s.credential.Config = appmodel.IntegrationConfig{Target: "owner/repo"}
	}
	s.credential.Generation = 1
	return s.credential, nil
}

func (s *integrationStoreStub) SyncExternalTasks(_ context.Context, request appmodel.IntegrationTaskSyncRequest) error {
	s.syncCalls++
	s.syncRequest = request
	return nil
}

type providerStub struct{ err error }

func (s providerStub) FetchTasks(context.Context, ProviderInput) ([]integrationport.ProviderTask, error) {
	return nil, s.err
}

type nilProviderStub struct{}

func (*nilProviderStub) FetchTasks(context.Context, ProviderInput) ([]integrationport.ProviderTask, error) {
	return nil, nil
}

type integrationAuditCall struct {
	team, actor              int64
	action, target, meta, ip string
}

type integrationAuditStub struct{ calls []integrationAuditCall }

func (s *integrationAuditStub) Record(_ context.Context, record model.AuditRecord) error {
	s.calls = append(s.calls, integrationAuditCall{record.TeamID, record.UserID, record.Action, record.Target, record.Meta, record.IP})
	return nil
}

type integrationLoggerStub struct{}

func (integrationLoggerStub) Printf(string, ...any) {}

func TestNewRejectsTypedNilProvider(t *testing.T) {
	store := &integrationStoreStub{}
	var provider *nilProviderStub
	_, err := New(Dependencies{
		Manager: store, Catalog: store, Tasks: store, SyncStarter: store,
		Audit: &integrationAuditStub{}, Logger: integrationLoggerStub{},
	}, provider)
	if !errors.Is(err, ErrIncompleteDependencies) {
		t.Fatalf("New() error = %v, want incomplete dependencies", err)
	}
}

func TestCredentialLifecycleAuditOmitsSecretAndRequiresSuccessfulWrite(t *testing.T) {
	store := &integrationStoreStub{}
	audit := &integrationAuditStub{}
	syncErr := errors.New("provider unavailable")
	service, err := New(Dependencies{
		Manager: store, Catalog: store, Tasks: store, SyncStarter: store,
		Audit: audit, Logger: integrationLoggerStub{},
	}, providerStub{err: syncErr})
	if err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithClientIP(context.Background(), "203.0.113.14")
	connected, err := service.ConnectAndSync(ctx, appmodel.IntegrationCreateRequest{
		TeamID: 5, CallerID: 8, Provider: "github", Name: "work", Secret: "super-secret", Config: appmodel.IntegrationConfig{Target: "owner/repo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if connected.Integration.ID != 17 || connected.Imported != 0 || !errors.Is(connected.SyncError, syncErr) {
		t.Fatalf("connect result = %+v, want created connection preserved with sync failure", connected)
	}
	if len(audit.calls) != 1 {
		t.Fatalf("create audit = %+v", audit.calls)
	}
	created := audit.calls[0]
	if created.team != 5 || created.actor != 8 || created.action != "integration.create" || created.target != "17" || created.meta != "github work" || created.ip != "203.0.113.14" || strings.Contains(created.meta, "super-secret") {
		t.Fatalf("credential-bearing create audit = %+v", created)
	}
	request := appmodel.IntegrationMutationRequest{TeamID: 5, IntegrationID: 17, CallerID: 8}
	if err := service.Delete(ctx, request); err != nil {
		t.Fatal(err)
	}
	if len(audit.calls) != 2 || audit.calls[1].action != "integration.delete" || audit.calls[1].target != "17" {
		t.Fatalf("delete audit = %+v", audit.calls)
	}
	store.deleteErr = model.ErrForbidden
	if err := service.Delete(ctx, request); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("failed delete error = %v", err)
	}
	if len(audit.calls) != 2 {
		t.Fatalf("failed delete recorded audit: %+v", audit.calls)
	}
}

func TestSyncTasksRejectsBoundarySnapshotBeforePersistence(t *testing.T) {
	store := &integrationStoreStub{}
	service, err := New(Dependencies{
		Manager: store, Catalog: store, Tasks: store, SyncStarter: store,
		Audit: &integrationAuditStub{}, Logger: integrationLoggerStub{},
	}, providerStub{})
	if err != nil {
		t.Fatal(err)
	}
	tasks := make([]integrationport.ProviderTask, MaxSyncTasks)
	for i := range tasks {
		tasks[i] = integrationport.ProviderTask{
			ExternalID: fmt.Sprintf("task-%d", i), Title: "Task", Status: "open",
		}
	}
	count, err := service.syncTasks(context.Background(), 5, 17, 8, tasks)
	if !errors.Is(err, ErrIncompleteSnapshot) {
		t.Fatalf("syncTasks error = %v, want incomplete snapshot", err)
	}
	if count != 0 || store.syncCalls != 0 {
		t.Fatalf("boundary snapshot reached persistence: count=%d calls=%d", count, store.syncCalls)
	}
}
