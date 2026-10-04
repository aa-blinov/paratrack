// Package payroll owns the workflow that turns tracked time into a payable
// snapshot. HTTP and future CLI/API adapters share this policy boundary.
package payroll

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
	"github.com/aa-blinov/paratrack/internal/postcommit"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// Store exposes only the persistence operations needed to create a run.
// CreatePayrollDraft allocates the number and writes the run and its lines in
// one workspace-locked transaction.
type Store interface {
	CreatePayrollDraft(context.Context, appmodel.PayrollDraftRequest) (model.PayrollRun, []model.PayrollRun, error)
	ListPayrollRunDetails(context.Context, appmodel.PayrollRunListQuery) ([]appmodel.PayrollRunDetails, error)
	GetPayrollRunDetails(context.Context, appmodel.PayrollRunLookupQuery) (appmodel.PayrollRunDetails, error)
	MarkPayrollPaidWithRecipients(context.Context, appmodel.PayrollMutationRequest) (bool, []int64, error)
	DeletePayrollDraft(context.Context, appmodel.PayrollMutationRequest) error
	SetMemberPay(context.Context, appmodel.PayrollMemberPayRequest) error
	ListMemberPayrollSettings(context.Context, int64) ([]model.MemberPayrollSettings, error)
}

type AuditRecorder interface {
	Record(context.Context, model.AuditRecord) error
}

type Logger interface {
	Printf(string, ...any)
}

// Service coordinates payroll creation policy with its persistence adapter.
type Service struct {
	store  Store
	now    func() time.Time
	audit  AuditRecorder
	logger Logger
}

var ErrIncompleteDependencies = errors.New("payroll service store is nil")

// NewServiceWithClock injects the clock used to price active sessions in a
// payroll snapshot. The same instant is persisted as the run creation time.
func NewServiceWithClock(store Store, now func() time.Time, audit AuditRecorder, logger Logger) (*Service, error) {
	if depcheck.IsNil(store) {
		return nil, ErrIncompleteDependencies
	}
	if now == nil {
		return nil, ErrIncompleteDependencies
	}
	if depcheck.IsNil(audit) || depcheck.IsNil(logger) {
		return nil, ErrIncompleteDependencies
	}
	return &Service{store: store, now: now, audit: audit, logger: logger}, nil
}

var (
	ErrInvalidTeam   = appmodel.ErrInvalidPayrollTeam
	ErrInvalidPeriod = appmodel.ErrInvalidPayrollPeriod
	ErrNoPayableTime = appmodel.ErrNoPayableTime
	ErrForbidden     = appmodel.ErrForbidden
	ErrNotFound      = appmodel.ErrNotFound
)

type RunDetails = appmodel.PayrollRunDetails

type PaidResult = appmodel.PayrollPaidResult

func (s *Service) ListRuns(ctx context.Context, query appmodel.PayrollRunListQuery) ([]appmodel.PayrollRunSummary, error) {
	if query.TeamID <= 0 {
		return nil, ErrInvalidTeam
	}
	runs, err := s.store.ListPayrollRunDetails(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list payroll run details: %w", err)
	}
	summaries := make([]appmodel.PayrollRunSummary, 0, len(runs))
	for _, details := range runs {
		totalCents, totalHours, err := payrollLineTotals(details.Lines)
		if err != nil {
			return nil, fmt.Errorf("summarize payroll run %d: %w", details.Run.ID, err)
		}
		summaries = append(summaries, appmodel.PayrollRunSummary{
			Run: details.Run, TotalCents: totalCents, TotalHoursHundredths: totalHours,
		})
	}
	return summaries, nil
}

func (s *Service) GetRun(ctx context.Context, teamID, runID int64) (appmodel.PayrollRunDetail, error) {
	if teamID <= 0 || runID <= 0 {
		return appmodel.PayrollRunDetail{}, ErrInvalidTeam
	}
	details, err := s.store.GetPayrollRunDetails(ctx, appmodel.PayrollRunLookupQuery{TeamID: teamID, RunID: runID})
	if err != nil {
		return appmodel.PayrollRunDetail{}, fmt.Errorf("get payroll run: %w", err)
	}
	totalCents, totalHours, err := payrollLineTotals(details.Lines)
	if err != nil {
		return appmodel.PayrollRunDetail{}, fmt.Errorf("summarize payroll run %d: %w", details.Run.ID, err)
	}
	return appmodel.PayrollRunDetail{
		Run: details.Run, Lines: details.Lines,
		TotalCents: totalCents, TotalHoursHundredths: totalHours,
	}, nil
}

func payrollLineTotals(lines []model.PayrollLine) (int, int, error) {
	totalCents, totalHours := 0, 0
	for _, line := range lines {
		var err error
		totalCents, err = money.AddCents(totalCents, line.AmountCents)
		if err != nil {
			return 0, 0, err
		}
		totalHours, err = money.AddInt(totalHours, money.HoursHundredths(line.Seconds))
		if err != nil {
			return 0, 0, err
		}
	}
	return totalCents, totalHours, nil
}

func (s *Service) MarkPaid(ctx context.Context, request appmodel.PayrollMutationRequest) (PaidResult, error) {
	if request.TeamID <= 0 {
		return PaidResult{}, ErrInvalidTeam
	}
	if request.RunID <= 0 || request.CallerID <= 0 {
		return PaidResult{}, ErrInvalidTeam
	}
	changed, recipients, err := s.store.MarkPayrollPaidWithRecipients(ctx, request)
	if err != nil {
		return PaidResult{}, fmt.Errorf("mark payroll paid: %w", err)
	}
	return PaidResult{Changed: changed, Recipients: recipients}, nil
}

func (s *Service) DeleteDraft(ctx context.Context, request appmodel.PayrollMutationRequest) error {
	if request.TeamID <= 0 {
		return ErrInvalidTeam
	}
	if request.RunID <= 0 || request.CallerID <= 0 {
		return ErrInvalidTeam
	}
	if err := s.store.DeletePayrollDraft(ctx, request); err != nil {
		return fmt.Errorf("delete payroll draft: %w", err)
	}
	return nil
}

func (s *Service) UpdateMemberPay(ctx context.Context, request appmodel.PayrollMemberPayRequest) error {
	if request.TeamID <= 0 {
		return ErrInvalidTeam
	}
	if request.UserID <= 0 || request.CallerID <= 0 {
		return ErrInvalidTeam
	}
	if request.PayCents != nil && *request.PayCents < 0 {
		return fmt.Errorf("pay must be non-negative")
	}
	if request.CapacityMinutes != nil && *request.CapacityMinutes < 0 {
		return fmt.Errorf("capacity must be non-negative")
	}
	if err := s.store.SetMemberPay(ctx, request); err != nil {
		return fmt.Errorf("update member pay: %w", err)
	}
	s.recordAudit(ctx, request.TeamID, request.CallerID, "member.pay_update", fmt.Sprint(request.UserID), "")
	return nil
}

// MemberSettings returns pay and capacity values for the current workspace roster.
func (s *Service) MemberSettings(ctx context.Context, teamID int64) ([]model.MemberPayrollSettings, error) {
	if teamID <= 0 {
		return nil, ErrInvalidTeam
	}
	settings, err := s.store.ListMemberPayrollSettings(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("list member payroll settings: %w", err)
	}
	return settings, nil
}

// CreateRun snapshots all payable time in [start, end) and creates a draft
// run. Its number and the run's lines are committed atomically by the store.
func (s *Service) CreateRun(ctx context.Context, request appmodel.PayrollDraftRequest) (model.PayrollRun, []model.PayrollRun, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return model.PayrollRun{}, nil, ErrInvalidTeam
	}
	if !request.End.After(request.Start) {
		return model.PayrollRun{}, nil, ErrInvalidPeriod
	}
	request.CreatedAt = s.now().UTC()
	run, overlaps, err := s.store.CreatePayrollDraft(ctx, request)
	if err != nil {
		return model.PayrollRun{}, nil, fmt.Errorf("create payroll draft: %w", err)
	}
	if run.ID > 0 {
		s.recordAudit(ctx, request.TeamID, request.CallerID, "payroll.create", run.Number, "")
	}
	return run, overlaps, nil
}

func (s *Service) recordAudit(ctx context.Context, teamID, actorID int64, action, target, meta string) {
	effectCtx, cancel := postcommit.NewContext(ctx)
	defer cancel()
	if err := s.audit.Record(effectCtx, model.AuditRecord{
		TeamID: teamID, UserID: actorID, Action: action, Target: target, Meta: meta, IP: requestctx.ClientIP(ctx),
	}); err != nil {
		s.logger.Printf("payroll: record %s audit for team %d: %v", action, teamID, err)
	}
}
