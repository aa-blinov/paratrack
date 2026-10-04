package audit

import (
	"context"
	"errors"
	"testing"

	"github.com/aa-blinov/paratrack/internal/model"
)

type storeStub struct {
	recordCalls int
	teamID      int64
	userID      int64
	action      string
	listCalls   int
	listTeamID  int64
	listLimit   int
	recordErr   error
	listErr     error
	entries     []model.AuditEntry
}

func (s *storeStub) Audit(_ context.Context, record model.AuditRecord) error {
	s.recordCalls++
	s.teamID, s.userID, s.action = record.TeamID, record.UserID, record.Action
	return s.recordErr
}

func (s *storeStub) ListAudit(_ context.Context, teamID int64, limit int) ([]model.AuditEntry, error) {
	s.listCalls++
	s.listTeamID, s.listLimit = teamID, limit
	return s.entries, s.listErr
}

func TestRecordRejectsInvalidInputBeforePersistence(t *testing.T) {
	store := &storeStub{}
	service, err := New(store)
	if err != nil {
		t.Fatal(err)
	}

	if err := service.Record(context.Background(), model.AuditRecord{UserID: 2, Action: "login"}); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("Record error = %v, want %v", err, ErrInvalidScope)
	}
	if err := service.Record(context.Background(), model.AuditRecord{TeamID: 1, UserID: 2}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("Record error = %v, want %v", err, ErrInvalidEvent)
	}
	if store.recordCalls != 0 {
		t.Fatalf("audit writes = %d, want 0", store.recordCalls)
	}
}

func TestRecordGlobalAllowsSystemScopeAndWrapsStoreErrors(t *testing.T) {
	writeErr := errors.New("database unavailable")
	store := &storeStub{recordErr: writeErr}
	service, err := New(store)
	if err != nil {
		t.Fatal(err)
	}

	if err := service.RecordGlobal(context.Background(), model.AuditRecord{TeamID: 4, Action: "auth.login"}); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("RecordGlobal with workspace scope error = %v, want %v", err, ErrInvalidScope)
	}
	err = service.RecordGlobal(context.Background(), model.AuditRecord{Action: "system.start"})
	if !errors.Is(err, writeErr) {
		t.Fatalf("RecordGlobal error = %v, want wrapped store error", err)
	}
	if store.recordCalls != 1 || store.teamID != 0 || store.userID != 0 || store.action != "system.start" {
		t.Fatalf("recorded scope/action = (%d, %d, %q), calls %d", store.teamID, store.userID, store.action, store.recordCalls)
	}
}

func TestListRequiresWorkspaceAndNormalizesLimit(t *testing.T) {
	store := &storeStub{entries: []model.AuditEntry{{ID: 3}}}
	service, err := New(store)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.List(context.Background(), 0, 25); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("List without workspace error = %v, want %v", err, ErrInvalidScope)
	}
	if store.listCalls != 0 {
		t.Fatalf("audit reads = %d, want 0", store.listCalls)
	}

	items, err := service.List(context.Background(), 7, 900)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != 3 {
		t.Fatalf("List items = %#v, want stored entries", items)
	}
	if store.listCalls != 1 || store.listTeamID != 7 || store.listLimit != 100 {
		t.Fatalf("list query = (team %d, limit %d, calls %d), want (7, 100, 1)", store.listTeamID, store.listLimit, store.listCalls)
	}
}
