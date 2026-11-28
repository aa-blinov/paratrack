// Package payroll owns the workflow that turns tracked time into a payable
// snapshot. HTTP and future CLI/API adapters share this policy boundary.
package payroll

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	ListMemberPayrollLines(context.Context, appmodel.MemberPayrollQuery) ([]appmodel.MemberPayrollLine, error)
	GetPayrollRunDetails(context.Context, appmodel.PayrollRunLookupQuery) (appmodel.PayrollRunDetails, error)
	MarkPayrollPaidWithRecipients(context.Context, appmodel.PayrollMutationRequest) (bool, []int64, error)
	UnmarkPayrollPaid(context.Context, appmodel.PayrollUnpayRequest) error
	DeletePayrollDraft(context.Context, appmodel.PayrollMutationRequest) error
	AddPayrollAdjustment(context.Context, appmodel.PayrollAdjustmentRequest) error
	DeletePayrollAdjustment(context.Context, appmodel.PayrollAdjustmentDeleteRequest) error
	RecalculatePayrollDraft(context.Context, appmodel.PayrollRecalculateRequest) (appmodel.PayrollRecalculateResult, error)
	PreviewPayrollRecalculation(context.Context, appmodel.PayrollRecalculateRequest) (appmodel.PayrollRecalculateResult, error)
	SetMemberPay(context.Context, appmodel.PayrollMemberPayRequest) error
	ListMemberPayrollSettings(context.Context, int64) ([]model.MemberPayrollSettings, error)
	UnapprovedPayrollTime(context.Context, int64, time.Time, time.Time) (*model.PayrollNotApprovedError, error)
	UnapprovedPayrollStarts(context.Context, int64, time.Time, time.Time) ([]time.Time, error)
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

type PaidResult = appmodel.PayrollPaidResult

// UnapprovedPayrollTime reports the closed time in [from, to) that is not in an
// approved week, so the page can say so before a run is refused.
func (s *Service) UnapprovedPayrollTime(ctx context.Context, teamID int64, from, to time.Time) (*model.PayrollNotApprovedError, error) {
	if teamID <= 0 {
		return nil, ErrInvalidTeam
	}
	notApproved, err := s.store.UnapprovedPayrollTime(ctx, teamID, from, to)
	if err != nil {
		return nil, fmt.Errorf("check approved payroll time: %w", err)
	}
	return notApproved, nil
}

// UnapprovedPayrollStarts lists the start of every session in [from, to) that is not in
// an approved week, oldest first, so the page can group them by week in one read.
func (s *Service) UnapprovedPayrollStarts(ctx context.Context, teamID int64, from, to time.Time) ([]time.Time, error) {
	if teamID <= 0 {
		return nil, ErrInvalidTeam
	}
	starts, err := s.store.UnapprovedPayrollStarts(ctx, teamID, from, to)
	if err != nil {
		return nil, fmt.Errorf("list unapproved payroll starts: %w", err)
	}
	return starts, nil
}

// MemberLines returns one person's own pay lines in the workspace, read-only (FIN-11).
func (s *Service) MemberLines(ctx context.Context, query appmodel.MemberPayrollQuery) ([]appmodel.MemberPayrollLine, error) {
	if query.TeamID <= 0 || query.UserID <= 0 {
		return nil, ErrInvalidTeam
	}
	lines, err := s.store.ListMemberPayrollLines(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list member payroll lines: %w", err)
	}
	return lines, nil
}

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
			Members: lineMembers(details.Lines),
		})
	}
	return summaries, nil
}

// lineMembers names each person a run prices, once, in the order the lines go.
func lineMembers(lines []model.PayrollLine) []string {
	seen := make(map[string]bool, len(lines))
	members := make([]string, 0, len(lines))
	for _, line := range lines {
		if seen[line.Label] {
			continue
		}
		seen[line.Label] = true
		members = append(members, line.Label)
	}
	return members
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

// Unpay takes a paid run back to a draft. The reason is required: an unmade
// payout is a correction the books ask about.
func (s *Service) Unpay(ctx context.Context, request appmodel.PayrollUnpayRequest) error {
	if request.TeamID <= 0 || request.RunID <= 0 || request.CallerID <= 0 {
		return ErrInvalidTeam
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" {
		return model.ErrPaymentReason
	}
	if err := s.store.UnmarkPayrollPaid(ctx, request); err != nil {
		return fmt.Errorf("unmark payroll paid: %w", err)
	}
	return nil
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
	if request.MonthlyPayCents != nil && *request.MonthlyPayCents < 0 {
		return fmt.Errorf("pay must be non-negative")
	}
	if request.Kind != nil && *request.Kind != "hourly" && *request.Kind != "monthly" {
		return fmt.Errorf("pay kind must be hourly or monthly")
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

// AddAdjustment adds a one-off bonus or deduction line to a draft pay run.
func (s *Service) AddAdjustment(ctx context.Context, request appmodel.PayrollAdjustmentRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return ErrInvalidTeam
	}
	if request.RunID <= 0 || request.UserID <= 0 {
		return ErrNotFound
	}
	request.Label = strings.TrimSpace(request.Label)
	if request.Label == "" || (request.AmountCents == 0 && request.PercentBps == 0) {
		return fmt.Errorf("adjustment label and non-zero amount or percent are required")
	}
	if err := s.store.AddPayrollAdjustment(ctx, request); err != nil {
		return fmt.Errorf("add payroll adjustment: %w", err)
	}
	s.recordAudit(ctx, request.TeamID, request.CallerID, "payroll.adjustment_add", fmt.Sprint(request.RunID), request.Label)
	return nil
}

// DeleteAdjustment removes a one-off adjustment line from a draft pay run.
func (s *Service) DeleteAdjustment(ctx context.Context, request appmodel.PayrollAdjustmentDeleteRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return ErrInvalidTeam
	}
	if request.RunID <= 0 || request.LineID <= 0 {
		return ErrNotFound
	}
	if err := s.store.DeletePayrollAdjustment(ctx, request); err != nil {
		return fmt.Errorf("delete payroll adjustment: %w", err)
	}
	s.recordAudit(ctx, request.TeamID, request.CallerID, "payroll.adjustment_delete", fmt.Sprint(request.RunID), fmt.Sprint(request.LineID))
	return nil
}

// RecalculateDraft prices a draft pay run again from the current timesheet,
// keeping fixed adjustments and re-taking percent ones of the new earnings.
func (s *Service) RecalculateDraft(ctx context.Context, request appmodel.PayrollRecalculateRequest) (appmodel.PayrollRecalculateResult, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return appmodel.PayrollRecalculateResult{}, ErrInvalidTeam
	}
	if request.RunID <= 0 {
		return appmodel.PayrollRecalculateResult{}, ErrNotFound
	}
	request.At = s.now()
	result, err := s.store.RecalculatePayrollDraft(ctx, request)
	if err != nil {
		return result, fmt.Errorf("recalculate payroll draft: %w", err)
	}
	// The journal keeps both totals, so a changed payout can be traced to
	// the recalculation that changed it.
	s.recordAudit(ctx, request.TeamID, request.CallerID, "payroll.recalculate", fmt.Sprint(request.RunID), recalculationAuditMeta(result))
	return result, nil
}

// PreviewRecalculation says what RecalculateDraft would change, per person,
// without changing anything.
func (s *Service) PreviewRecalculation(ctx context.Context, request appmodel.PayrollRecalculateRequest) (appmodel.PayrollRecalculateResult, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return appmodel.PayrollRecalculateResult{}, ErrInvalidTeam
	}
	if request.RunID <= 0 {
		return appmodel.PayrollRecalculateResult{}, ErrNotFound
	}
	request.At = s.now()
	result, err := s.store.PreviewPayrollRecalculation(ctx, request)
	if err != nil {
		return result, fmt.Errorf("preview payroll recalculation: %w", err)
	}
	return result, nil
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

// recalculationAuditMeta writes what a recalculation changed for the journal:
// the totals, then each person whose amount moved, in cents.
func recalculationAuditMeta(result appmodel.PayrollRecalculateResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d -> %d", result.BeforeCents, result.AfterCents)
	for _, p := range result.People {
		if p.BeforeCents != p.AfterCents || p.BeforeSeconds != p.AfterSeconds {
			fmt.Fprintf(&b, "; %s (#%d): %d -> %d (%ds -> %ds)", p.Name, p.UserID, p.BeforeCents, p.AfterCents, p.BeforeSeconds, p.AfterSeconds)
		}
	}
	return b.String()
}
