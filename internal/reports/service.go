// Package reports owns reusable statistics-filter presets shared by adapters.
package reports

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

var (
	ErrInvalidSavedReport     = appmodel.ErrInvalidSavedReport
	ErrIncompleteDependencies = errors.New("reports service store is nil")
)

type Store interface {
	CreateSavedReport(context.Context, appmodel.SavedReportCreateRequest) (model.SavedReport, error)
	ListSavedReports(context.Context, int64) ([]model.SavedReport, error)
	DeleteSavedReportForActor(context.Context, appmodel.SavedReportDeleteRequest) error
}

type Service struct{ store Store }

func New(store Store) (*Service, error) {
	if depcheck.IsNil(store) {
		return nil, ErrIncompleteDependencies
	}
	return &Service{store: store}, nil
}

func (s *Service) Create(ctx context.Context, request appmodel.SavedReportCreateRequest) (model.SavedReport, error) {
	request.Name, request.Period = strings.TrimSpace(request.Name), strings.TrimSpace(request.Period)
	if request.TeamID <= 0 || request.ActorID <= 0 || request.Name == "" || len([]rune(request.Name)) > 40 {
		return model.SavedReport{}, ErrInvalidSavedReport
	}
	if request.Period == "" {
		request.Period = "today"
	}
	if !timeparse.IsKnownPeriod(request.Period) {
		return model.SavedReport{}, fmt.Errorf("%w: unsupported period", ErrInvalidSavedReport)
	}
	request.ProjectSlug, request.Tag = strings.TrimSpace(request.ProjectSlug), strings.TrimSpace(request.Tag)
	report, err := s.store.CreateSavedReport(ctx, request)
	if err != nil {
		return model.SavedReport{}, fmt.Errorf("create saved report: %w", err)
	}
	return report, nil
}

func (s *Service) List(ctx context.Context, teamID int64) ([]model.SavedReport, error) {
	if teamID <= 0 {
		return nil, ErrInvalidSavedReport
	}
	reports, err := s.store.ListSavedReports(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("list saved reports: %w", err)
	}
	return reports, nil
}

func (s *Service) Delete(ctx context.Context, request appmodel.SavedReportDeleteRequest) error {
	if request.TeamID <= 0 || request.ReportID <= 0 || request.CallerID <= 0 {
		return ErrInvalidSavedReport
	}
	if err := s.store.DeleteSavedReportForActor(ctx, request); err != nil {
		return fmt.Errorf("delete saved report: %w", err)
	}
	return nil
}
