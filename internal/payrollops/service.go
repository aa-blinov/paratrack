// Package payrollops coordinates payroll state changes with application side
// effects that belong to a successful payment transition.
package payrollops

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/postcommit"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

var ErrIncompleteDependencies = errors.New("payroll operations dependencies are incomplete")

type PaymentWorkflow interface {
	MarkPaid(context.Context, appmodel.PayrollMutationRequest) (appmodel.PayrollPaidResult, error)
}

type AuditRecorder interface {
	Record(context.Context, model.AuditRecord) error
}

type NotificationEnqueuer interface {
	EnqueuePayrollPaid(context.Context, appmodel.PayrollPaidNotification) error
}

type Logger interface {
	Printf(string, ...any)
}

type Dependencies struct {
	Payments      PaymentWorkflow
	Audit         AuditRecorder
	Notifications NotificationEnqueuer
	Logger        Logger
}

type Service struct {
	payments      PaymentWorkflow
	audit         AuditRecorder
	notifications NotificationEnqueuer
	logger        Logger
}

func New(deps Dependencies) (*Service, error) {
	for _, dependency := range []struct {
		name string
		port any
	}{
		{"payment workflow", deps.Payments},
		{"audit recorder", deps.Audit},
		{"notification enqueuer", deps.Notifications},
		{"logger", deps.Logger},
	} {
		if depcheck.IsNil(dependency.port) {
			return nil, fmt.Errorf("%w: %s", ErrIncompleteDependencies, dependency.name)
		}
	}
	return &Service{payments: deps.Payments, audit: deps.Audit, notifications: deps.Notifications, logger: deps.Logger}, nil
}

// MarkPaid records side effects only for the request that changes the run to
// paid. Payment remains authoritative if best-effort audit or notification
// delivery cannot be recorded.
func (s *Service) MarkPaid(ctx context.Context, request appmodel.PayrollMutationRequest) error {
	if request.TeamID <= 0 {
		return model.ErrNotFound
	}
	teamID, runID, callerID := request.TeamID, request.RunID, request.CallerID
	if runID <= 0 || callerID <= 0 {
		return model.ErrNotFound
	}
	result, err := s.payments.MarkPaid(ctx, request)
	if err != nil {
		return err
	}
	if !result.Changed {
		return nil
	}
	effectCtx, cancel := postcommit.NewContext(ctx)
	defer cancel()
	if err := s.audit.Record(effectCtx, model.AuditRecord{
		TeamID: teamID, UserID: callerID, Action: "payroll.paid", Target: strconv.FormatInt(runID, 10), IP: requestctx.ClientIP(ctx),
	}); err != nil {
		s.logger.Printf("payroll: record paid audit for run %d: %v", runID, err)
	}
	if err := s.notifications.EnqueuePayrollPaid(effectCtx, appmodel.PayrollPaidNotification{
		TeamID: teamID, Recipients: result.Recipients,
	}); err != nil {
		s.logger.Printf("payroll: enqueue paid notification for run %d: %v", runID, err)
	}
	return nil
}
