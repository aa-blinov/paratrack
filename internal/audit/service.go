// Package audit owns account and workspace audit-event recording.
package audit

import (
	"context"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
)

type Store interface {
	Audit(context.Context, model.AuditRecord) error
	ListAudit(context.Context, int64, int) ([]model.AuditEntry, error)
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

func (s *Service) List(ctx context.Context, teamID int64, limit int) ([]model.AuditEntry, error) {
	if teamID <= 0 {
		return nil, ErrInvalidScope
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	items, err := s.store.ListAudit(ctx, teamID, limit)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	return items, nil
}
