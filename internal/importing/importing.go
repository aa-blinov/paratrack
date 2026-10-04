// Package importing coordinates provider fetches with validated, atomic
// application of imported session batches.
package importing

import (
	"context"
	"errors"
	"fmt"
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/importport"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/postcommit"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

var (
	ErrInvalidEntry           = importport.ErrInvalidEntry
	ErrEntryLimit             = importport.ErrEntryLimit
	ErrApplyFailed            = importport.ErrApplyFailed
	ErrInvalidInput           = importport.ErrInvalidInput
	ErrPaginationLimit        = importport.ErrPaginationLimit
	ErrIncompleteDependencies = errors.New("importing service dependencies are incomplete")
)

// Store applies a complete import batch atomically and skips external IDs
// that have already been imported into the workspace.
type Store interface {
	ImportEntries(context.Context, appmodel.ImportBatchRequest) (appmodel.ImportResult, error)
}

type ProviderFetcher = importport.ProviderFetcher

type AuditRecorder interface {
	Record(context.Context, model.AuditRecord) error
}

type Logger interface {
	Printf(string, ...any)
}

type Service struct {
	store     Store
	providers ProviderFetcher
	audit     AuditRecorder
	logger    Logger
}

func New(store Store, providers ProviderFetcher, audit AuditRecorder, logger Logger) (*Service, error) {
	if depcheck.IsNil(store) || depcheck.IsNil(providers) || depcheck.IsNil(audit) || depcheck.IsNil(logger) {
		return nil, ErrIncompleteDependencies
	}
	return &Service{store: store, providers: providers, audit: audit, logger: logger}, nil
}

// Preview fetches and normalizes a provider's entries without persisting them.
func (s *Service) Preview(ctx context.Context, request importport.ProviderRequest) ([]importport.ImportedEntry, error) {
	entries, err := s.providers.Fetch(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("fetch import provider entries: %w", err)
	}
	clean, err := validateEntries(entries)
	if err != nil {
		return nil, fmt.Errorf("validate import provider entries: %w", err)
	}
	return clean, nil
}

// RunFromProvider fetches entries and applies them as one validated import
// batch. No local writes occur unless fetching and validation both succeed.
func (s *Service) RunFromProvider(ctx context.Context, request appmodel.ProviderImportRunRequest) (appmodel.ImportResult, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return appmodel.ImportResult{}, fmt.Errorf("%w: team and caller IDs must be positive", ErrInvalidEntry)
	}
	teamID, callerID := request.TeamID, request.CallerID
	entries, err := s.Preview(ctx, request.Provider)
	if err != nil {
		return appmodel.ImportResult{}, err
	}
	result, err := s.store.ImportEntries(ctx, appmodel.ImportBatchRequest{
		TeamID: teamID, CallerID: callerID, Provider: request.Provider.Provider, Entries: entries,
	})
	if err != nil {
		return appmodel.ImportResult{}, err
	}
	effectCtx, cancel := postcommit.NewContext(ctx)
	defer cancel()
	if err := s.audit.Record(effectCtx, model.AuditRecord{
		TeamID: teamID, UserID: callerID, Action: "import.run", Target: request.Provider.Provider,
		Meta: fmt.Sprintf("%d", result.Imported), IP: requestctx.ClientIP(ctx),
	}); err != nil {
		s.logger.Printf("importing: record provider import audit for team %d: %v", teamID, err)
	}
	return result, nil
}

// runEntries validates and applies an already fetched batch.
func (s *Service) runEntries(ctx context.Context, teamID, callerID int64, entries []importport.ImportedEntry) (appmodel.ImportResult, error) {
	if teamID <= 0 || callerID <= 0 {
		return appmodel.ImportResult{}, fmt.Errorf("%w: team and caller IDs must be positive", ErrInvalidEntry)
	}
	clean, err := validateEntries(entries)
	if err != nil {
		return appmodel.ImportResult{}, err
	}
	result, err := s.store.ImportEntries(ctx, appmodel.ImportBatchRequest{TeamID: teamID, CallerID: callerID, Entries: clean})
	if err != nil {
		return appmodel.ImportResult{}, fmt.Errorf("%w: %w", ErrApplyFailed, err)
	}
	return result, nil
}

func validateEntries(entries []importport.ImportedEntry) ([]importport.ImportedEntry, error) {
	if len(entries) > importport.MaxEntries {
		return nil, ErrEntryLimit
	}
	clean := make([]importport.ImportedEntry, len(entries))
	copy(clean, entries)
	maxDuration := time.Duration(model.MaxSessionDurationSeconds) * time.Second
	for i := range clean {
		clean[i].Activity = strings.TrimSpace(clean[i].Activity)
		clean[i].ExternalID = strings.TrimSpace(clean[i].ExternalID)
		if clean[i].Activity == "" || clean[i].Start.IsZero() || !clean[i].End.After(clean[i].Start) ||
			clean[i].End.After(clean[i].Start.Add(maxDuration)) {
			return nil, fmt.Errorf("%w at index %d", ErrInvalidEntry, i)
		}
	}
	return clean, nil
}
