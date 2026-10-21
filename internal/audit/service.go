// Package audit owns account and workspace audit-event recording.
package audit

import (
	"context"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
)

type Store interface {
	Audit(context.Context, model.AuditRecord) error
	ListAudit(context.Context, appmodel.AuditListQuery) ([]model.AuditEntry, error)
	ListAuditActors(context.Context, int64) ([]int64, error)
	UserLabels(context.Context, []int64) (map[int64]string, error)
}

type Service struct{ store Store }

var (
	ErrInvalidScope           = errors.New("audit scope requires a positive workspace ID and non-negative actor IDs")
	ErrInvalidEvent           = errors.New("audit action is required")
	ErrIncompleteDependencies = errors.New("audit service store is nil")
)

func New(store Store) (*Service, error) {
	if depcheck.IsNil(store) {
		return nil, ErrIncompleteDependencies
	}
	return &Service{store: store}, nil
}

func (s *Service) Record(ctx context.Context, record model.AuditRecord) error {
	if record.TeamID <= 0 || record.UserID < 0 {
		return ErrInvalidScope
	}
	if record.Action == "" {
		return ErrInvalidEvent
	}
	if err := s.store.Audit(ctx, record); err != nil {
		return fmt.Errorf("record audit event: %w", err)
	}
	return nil
}

// RecordGlobal stores an account-level event that occurs before a workspace
// is selected, such as a successful login, or a system event with userID zero.
// Workspace events must use Record.
func (s *Service) RecordGlobal(ctx context.Context, record model.AuditRecord) error {
	if record.TeamID != 0 || record.UserID < 0 {
		return ErrInvalidScope
	}
	if record.Action == "" {
		return ErrInvalidEvent
	}
	if err := s.store.Audit(ctx, record); err != nil {
		return fmt.Errorf("record global audit event: %w", err)
	}
	return nil
}

func (s *Service) List(ctx context.Context, query appmodel.AuditListQuery) ([]model.AuditEntry, error) {
	if query.TeamID <= 0 {
		return nil, ErrInvalidScope
	}
	if query.Limit <= 0 || query.Limit > 500 {
		query.Limit = 100
	}
	items, err := s.store.ListAudit(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	return items, nil
}

// Actors lists everyone who left a row in the workspace's journal, former members
// included, so the journal's filter can offer them.
func (s *Service) Actors(ctx context.Context, teamID int64) ([]int64, error) {
	if teamID <= 0 {
		return nil, ErrInvalidScope
	}
	ids, err := s.store.ListAuditActors(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("list audit actors: %w", err)
	}
	return ids, nil
}

// Labels names users for the journal. Ids without an account are left out.
func (s *Service) Labels(ctx context.Context, ids []int64) (map[int64]string, error) {
	labels, err := s.store.UserLabels(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("name audit users: %w", err)
	}
	return labels, nil
}
