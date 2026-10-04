package payrollops

import (
	"context"
	"io"
	"log"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

type paymentStub struct {
	results []appmodel.PayrollPaidResult
	calls   int
}

func (s *paymentStub) MarkPaid(context.Context, appmodel.PayrollMutationRequest) (appmodel.PayrollPaidResult, error) {
	s.calls++
	result := s.results[0]
	s.results = s.results[1:]
	return result, nil
}

func TestMarkPaidRejectsInvalidScopeBeforeCallingPaymentWorkflow(t *testing.T) {
	payments := &paymentStub{}
	audit, notifications := &auditStub{}, &notificationStub{}
	service, err := New(Dependencies{
		Payments: payments, Audit: audit, Notifications: notifications,
		Logger: log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, input := range []struct {
		teamID, runID, callerID int64
	}{
		{teamID: 0, runID: 12, callerID: 5},
		{teamID: 3, runID: 0, callerID: 5},
		{teamID: 3, runID: 12, callerID: 0},
	} {
		if err := service.MarkPaid(context.Background(), appmodel.PayrollMutationRequest{TeamID: input.teamID, RunID: input.runID, CallerID: input.callerID}); err != model.ErrNotFound {
			t.Fatalf("MarkPaid(%d, %d, %d) error = %v, want %v", input.teamID, input.runID, input.callerID, err, model.ErrNotFound)
		}
	}
	if payments.calls != 0 || audit.calls != 0 || notifications.calls != 0 {
		t.Fatalf("invalid scopes reached payment/audit/notifications: %d/%d/%d", payments.calls, audit.calls, notifications.calls)
	}
}

type auditStub struct {
	calls  int
	team   int64
	actor  int64
	act    string
	target string
	ip     string
}

func (s *auditStub) Record(_ context.Context, record model.AuditRecord) error {
	s.calls++
	s.team, s.actor, s.act, s.target, s.ip = record.TeamID, record.UserID, record.Action, record.Target, record.IP
	return nil
}

type notificationStub struct{ calls int }

func (s *notificationStub) EnqueuePayrollPaid(context.Context, appmodel.PayrollPaidNotification) error {
	s.calls++
	return nil
}

func TestMarkPaidPublishesSideEffectsOnlyForTransition(t *testing.T) {
	payments := &paymentStub{results: []appmodel.PayrollPaidResult{
		{Changed: true, Recipients: []int64{8, 9}},
		{Changed: false},
	}}
	audit, notifications := &auditStub{}, &notificationStub{}
	service, err := New(Dependencies{
		Payments: payments, Audit: audit, Notifications: notifications,
		Logger: log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithClientIP(context.Background(), "203.0.113.4")
	request := appmodel.PayrollMutationRequest{TeamID: 3, RunID: 12, CallerID: 5}
	if err := service.MarkPaid(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := service.MarkPaid(ctx, request); err != nil {
		t.Fatal(err)
	}
	if audit.calls != 1 || audit.team != 3 || audit.actor != 5 || audit.act != "payroll.paid" || audit.target != "12" || audit.ip != "203.0.113.4" {
		t.Fatalf("audit calls=%d values=(%d,%d,%q,%q,%q)", audit.calls, audit.team, audit.actor, audit.act, audit.target, audit.ip)
	}
	if notifications.calls != 1 {
		t.Fatalf("notification calls = %d, want 1", notifications.calls)
	}
}
