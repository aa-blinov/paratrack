package billing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

type paymentProcessorStub struct {
	teamID, invoiceID int64
	changed           bool
	err               error
}

type manualPaymentSpy struct{ calls int }

func (s *manualPaymentSpy) RecordPayment(context.Context, appmodel.ManualPaymentRequest) (string, bool, error) {
	s.calls++
	return "INV-12", true, nil
}

func (s paymentProcessorStub) ProcessStripeWebhook(context.Context, appmodel.StripePaymentEvent) (int64, int64, bool, error) {
	return s.teamID, s.invoiceID, s.changed, s.err
}

func (s paymentProcessorStub) RecordPayment(context.Context, appmodel.ManualPaymentRequest) (string, bool, error) {
	return "INV-12", s.changed, s.err
}

type auditRecorderStub struct {
	calls int
	args  []any
	err   error
}

func (s *auditRecorderStub) Record(_ context.Context, record model.AuditRecord) error {
	s.calls++
	s.args = []any{record.TeamID, record.UserID, record.Action, record.Target, record.Meta, record.IP}
	return s.err
}

type loggerStub struct{ calls int }

func (s *loggerStub) Printf(string, ...any) { s.calls++ }

func TestProcessStripeWebhookAuditsOnlyAfterInvoiceTransition(t *testing.T) {
	audit := &auditRecorderStub{}
	logger := &loggerStub{}
	service, err := New(Dependencies{
		Payments:       paymentProcessorStub{teamID: 4, invoiceID: 12},
		ManualPayments: paymentProcessorStub{}, Audit: audit, Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ProcessStripeWebhook(context.Background(), appmodel.StripeWebhookRequest{
		Event: appmodel.StripePaymentEvent{Signature: "signature", Body: []byte("body"), ReceivedAt: time.Now()}, ClientIP: "192.0.2.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || audit.calls != 0 {
		t.Fatalf("unchanged payment result=%+v audit=%d", result, audit.calls)
	}

	service.payments = paymentProcessorStub{teamID: 4, invoiceID: 12, changed: true}
	result, err = service.ProcessStripeWebhook(context.Background(), appmodel.StripeWebhookRequest{
		Event: appmodel.StripePaymentEvent{Signature: "signature", Body: []byte("body"), ReceivedAt: time.Now()}, ClientIP: "192.0.2.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || audit.calls != 1 {
		t.Fatalf("changed payment result=%+v audit=%d", result, audit.calls)
	}
	if audit.args[0] != int64(4) || audit.args[1] != int64(0) || audit.args[2] != "invoice.paid" || audit.args[3] != "12" || audit.args[4] != "stripe" || audit.args[5] != "192.0.2.1" {
		t.Fatalf("audit args = %v", audit.args)
	}
}

func TestProcessStripeWebhookKeepsCommittedPaymentSuccessfulOnSideEffectFailure(t *testing.T) {
	audit := &auditRecorderStub{err: errors.New("audit unavailable")}
	logger := &loggerStub{}
	service, err := New(Dependencies{
		Payments:       paymentProcessorStub{teamID: 4, invoiceID: 12, changed: true},
		ManualPayments: paymentProcessorStub{}, Audit: audit, Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ProcessStripeWebhook(context.Background(), appmodel.StripeWebhookRequest{
		Event: appmodel.StripePaymentEvent{Signature: "signature"},
	})
	if err != nil || !result.Changed {
		t.Fatalf("committed payment result=%+v error=%v", result, err)
	}
	if audit.calls != 1 || logger.calls != 1 {
		t.Fatalf("audit effect calls=%d logged=%d", audit.calls, logger.calls)
	}
}

func TestRecordManualPaymentAuditsOnlyOnTransition(t *testing.T) {
	audit, logger := &auditRecorderStub{}, &loggerStub{}
	service, err := New(Dependencies{
		ManualPayments: paymentProcessorStub{changed: true},
		Payments:       paymentProcessorStub{}, Audit: audit, Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := appmodel.ManualPaymentRequest{TeamID: 4, InvoiceID: 12, CallerID: 5, ClientIP: "192.0.2.1"}
	if err := service.RecordManualPayment(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	service.manualPayments = paymentProcessorStub{changed: false}
	if err := service.RecordManualPayment(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if audit.calls != 1 || audit.args[0] != int64(4) || audit.args[1] != int64(5) || audit.args[2] != "invoice.paid" || audit.args[3] != "INV-12" || audit.args[4] != "manual" || audit.args[5] != "192.0.2.1" {
		t.Fatalf("audit args = %v", audit.args)
	}
}

func TestRecordManualPaymentRejectsInvalidScopeBeforeCallingPaymentWorkflow(t *testing.T) {
	manualPayments := &manualPaymentSpy{}
	audit, logger := &auditRecorderStub{}, &loggerStub{}
	service, err := New(Dependencies{
		Payments: paymentProcessorStub{}, ManualPayments: manualPayments,
		Audit: audit, Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, input := range []struct {
		teamID, invoiceID, callerID int64
	}{
		{teamID: 0, invoiceID: 12, callerID: 5},
		{teamID: 4, invoiceID: 0, callerID: 5},
		{teamID: 4, invoiceID: 12, callerID: 0},
	} {
		err := service.RecordManualPayment(context.Background(), appmodel.ManualPaymentRequest{
			TeamID: input.teamID, InvoiceID: input.invoiceID, CallerID: input.callerID,
		})
		if err != model.ErrNotFound {
			t.Fatalf("RecordManualPayment(%d, %d, %d) error = %v, want %v", input.teamID, input.invoiceID, input.callerID, err, model.ErrNotFound)
		}
	}
	if manualPayments.calls != 0 || audit.calls != 0 {
		t.Fatalf("invalid scopes reached payment/audit: %d/%d", manualPayments.calls, audit.calls)
	}
}
