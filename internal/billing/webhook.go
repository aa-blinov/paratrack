// Package billing coordinates payment workflows and their application events.
package billing

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/postcommit"
)

var ErrIncompleteDependencies = errors.New("billing service dependencies are incomplete")

type StripePaymentProcessor interface {
	ProcessStripeWebhook(context.Context, appmodel.StripePaymentEvent) (int64, int64, bool, error)
}

type ManualPaymentProcessor interface {
	RecordPayment(context.Context, appmodel.ManualPaymentRequest) (string, bool, error)
}

type AuditRecorder interface {
	Record(context.Context, model.AuditRecord) error
}

type Logger interface {
	Printf(string, ...any)
}

type Dependencies struct {
	Payments       StripePaymentProcessor
	ManualPayments ManualPaymentProcessor
	Audit          AuditRecorder
	Logger         Logger
}

type Service struct {
	payments       StripePaymentProcessor
	manualPayments ManualPaymentProcessor
	audit          AuditRecorder
	logger         Logger
}

type StripeWebhookResult = appmodel.StripeWebhookResult

func New(deps Dependencies) (*Service, error) {
	if depcheck.IsNil(deps.Payments) {
		return nil, fmt.Errorf("%w: Stripe payment processor", ErrIncompleteDependencies)
	}
	if depcheck.IsNil(deps.ManualPayments) {
		return nil, fmt.Errorf("%w: manual payment processor", ErrIncompleteDependencies)
	}
	if depcheck.IsNil(deps.Audit) {
		return nil, fmt.Errorf("%w: audit recorder", ErrIncompleteDependencies)
	}
	if depcheck.IsNil(deps.Logger) {
		return nil, fmt.Errorf("%w: logger", ErrIncompleteDependencies)
	}
	return &Service{payments: deps.Payments, manualPayments: deps.ManualPayments, audit: deps.Audit, logger: deps.Logger}, nil
}

// ProcessStripeWebhook applies a verified payment and publishes application
// side effects only when the invoice transitioned to paid. Provider retries
// remain idempotent because the invoice workflow reports whether it changed.
func (s *Service) ProcessStripeWebhook(ctx context.Context, request appmodel.StripeWebhookRequest) (StripeWebhookResult, error) {
	teamID, invoiceID, changed, err := s.payments.ProcessStripeWebhook(ctx, request.Event)
	if err != nil {
		return StripeWebhookResult{}, err
	}
	result := StripeWebhookResult{TeamID: teamID, InvoiceID: invoiceID, Changed: changed}
	if !changed {
		return result, nil
	}
	s.publishPaid(ctx, teamID, invoiceID, 0, "stripe", strconv.FormatInt(invoiceID, 10), request.ClientIP)
	return result, nil
}

// RecordManualPayment applies a user-confirmed payment and publishes the same
// paid event as provider payments, once per committed transition.
func (s *Service) RecordManualPayment(ctx context.Context, request appmodel.ManualPaymentRequest) error {
	if request.TeamID <= 0 || request.InvoiceID <= 0 || request.CallerID <= 0 {
		return model.ErrNotFound
	}
	number, changed, err := s.manualPayments.RecordPayment(ctx, request)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	s.publishPaid(ctx, request.TeamID, request.InvoiceID, request.CallerID, "manual", number, request.ClientIP)
	return nil
}

func (s *Service) publishPaid(ctx context.Context, teamID, invoiceID, actorID int64, source, target, clientIP string) {
	effectCtx, cancel := postcommit.NewContext(ctx)
	defer cancel()
	if err := s.audit.Record(effectCtx, model.AuditRecord{
		TeamID: teamID, UserID: actorID, Action: "invoice.paid", Target: target, Meta: source, IP: clientIP,
	}); err != nil {
		s.logger.Printf("billing: record %s payment audit for invoice %d: %v", source, invoiceID, err)
	}
}
