package importing

import (
	"context"
	"errors"
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/importport"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

type importStore struct {
	calls    int
	entries  []importport.ImportedEntry
	provider string
}

func (s *importStore) ImportEntries(_ context.Context, request appmodel.ImportBatchRequest) (appmodel.ImportResult, error) {
	s.calls++
	s.provider = request.Provider
	s.entries = request.Entries
	return appmodel.ImportResult{Imported: len(request.Entries)}, nil
}

type providerFetcherFunc func(context.Context, importport.ProviderRequest) ([]importport.ImportedEntry, error)

type importAudit struct {
	calls  int
	teamID int64
	actor  int64
	action string
	target string
	meta   string
	ip     string
}

func (s *importAudit) Record(_ context.Context, record model.AuditRecord) error {
	s.calls++
	s.teamID, s.actor, s.action, s.target, s.meta, s.ip = record.TeamID, record.UserID, record.Action, record.Target, record.Meta, record.IP
	return nil
}

type importLogger struct{}

func (importLogger) Printf(string, ...any) {}

func (f providerFetcherFunc) Fetch(ctx context.Context, request importport.ProviderRequest) ([]importport.ImportedEntry, error) {
	return f(ctx, request)
}

func TestRunFromProviderFetchesThenAppliesValidatedBatch(t *testing.T) {
	store := &importStore{}
	start := time.Date(2025, time.January, 1, 9, 0, 0, 0, time.UTC)
	want := []importport.ImportedEntry{{ExternalID: "toggl:1", Activity: "Design", Start: start, End: start.Add(time.Hour)}}
	audit := &importAudit{}
	service, err := New(store, providerFetcherFunc(func(_ context.Context, request importport.ProviderRequest) ([]importport.ImportedEntry, error) {
		if request.Provider != "toggl" || request.Secret != "secret" || request.Extra != "workspace" || request.From != "from" || request.To != "to" || request.Timezone != "UTC" {
			t.Fatalf("provider request = %+v", request)
		}
		return want, nil
	}), audit, importLogger{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := requestctx.WithClientIP(context.Background(), "203.0.113.7")
	result, err := service.RunFromProvider(ctx, appmodel.ProviderImportRunRequest{TeamID: 7, CallerID: 11, Provider: importport.ProviderRequest{
		Provider: "toggl", Secret: "secret", Extra: "workspace", From: "from", To: "to", Timezone: "UTC",
	}})
	if err != nil {
		t.Fatalf("RunFromProvider: %v", err)
	}
	if result.Imported != 1 || store.calls != 1 || store.provider != "toggl" || len(store.entries) != 1 || store.entries[0].ExternalID != want[0].ExternalID {
		t.Fatalf("result=%+v store=%+v", result, store)
	}
	if audit.calls != 1 || audit.teamID != 7 || audit.actor != 11 || audit.action != "import.run" || audit.target != "toggl" || audit.meta != "1" || audit.ip != "203.0.113.7" {
		t.Fatalf("audit = %+v", audit)
	}
}

func TestRunFromProviderDoesNotWriteAfterFetchFailure(t *testing.T) {
	store := &importStore{}
	fetchErr := errors.New("provider unavailable")
	audit := &importAudit{}
	service, err := New(store, providerFetcherFunc(func(context.Context, importport.ProviderRequest) ([]importport.ImportedEntry, error) {
		return nil, fetchErr
	}), audit, importLogger{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := service.RunFromProvider(context.Background(), appmodel.ProviderImportRunRequest{TeamID: 7, CallerID: 11, Provider: importport.ProviderRequest{Provider: "toggl", Secret: "secret"}}); !errors.Is(err, fetchErr) {
		t.Fatalf("RunFromProvider error = %v, want provider error", err)
	}
	if store.calls != 0 {
		t.Fatalf("store calls = %d, want 0", store.calls)
	}
	if audit.calls != 0 {
		t.Fatalf("failed fetch emitted audit effects: %d", audit.calls)
	}
}

func TestPreviewRejectsInvalidProviderEntries(t *testing.T) {
	start := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	tooLong := start.Add(time.Duration(model.MaxSessionDurationSeconds)*time.Second + time.Nanosecond)
	service, err := New(&importStore{}, providerFetcherFunc(func(context.Context, importport.ProviderRequest) ([]importport.ImportedEntry, error) {
		return []importport.ImportedEntry{{Activity: "Design", Start: start, End: tooLong}}, nil
	}), &importAudit{}, importLogger{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := service.Preview(context.Background(), importport.ProviderRequest{Provider: "provider"}); !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("Preview error = %v, want ErrInvalidEntry", err)
	}
}

func TestRunRejectsImportBatchOverEntryLimit(t *testing.T) {
	store := &importStore{}
	service, err := New(store, providerFetcherFunc(func(context.Context, importport.ProviderRequest) ([]importport.ImportedEntry, error) {
		return nil, nil
	}), &importAudit{}, importLogger{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	entries := make([]importport.ImportedEntry, importport.MaxEntries+1)
	if _, err := service.runEntries(context.Background(), 7, 11, entries); !errors.Is(err, ErrEntryLimit) {
		t.Fatalf("Run error = %v, want ErrEntryLimit", err)
	}
	if store.calls != 0 {
		t.Fatalf("store calls = %d, want 0", store.calls)
	}
}

func TestRunRejectsSessionsLongerThanRepresentableDuration(t *testing.T) {
	store := &importStore{}
	service, err := New(store, providerFetcherFunc(func(context.Context, importport.ProviderRequest) ([]importport.ImportedEntry, error) {
		return nil, nil
	}), &importAudit{}, importLogger{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	start := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	maxDuration := time.Duration(model.MaxSessionDurationSeconds) * time.Second
	entries := []importport.ImportedEntry{{Activity: "Design", Start: start, End: start.Add(maxDuration + time.Nanosecond)}}
	if _, err := service.runEntries(context.Background(), 7, 11, entries); !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("Run error = %v, want ErrInvalidEntry", err)
	}
	if store.calls != 0 {
		t.Fatalf("store calls = %d, want 0", store.calls)
	}
}
