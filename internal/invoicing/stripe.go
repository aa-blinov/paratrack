package invoicing

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
	"github.com/aa-blinov/paratrack/internal/postcommit"
)

func (s *Service) CreateStripePaymentLink(ctx context.Context, request appmodel.InvoiceStripeLinkRequest) (StripePaymentLink, error) {
	if request.TeamID <= 0 {
		return StripePaymentLink{}, ErrInvalidInvoice
	}
	teamID, invoiceID, callerID, successURL := request.TeamID, request.InvoiceID, request.CallerID, request.SuccessURL
	if teamID <= 0 || invoiceID <= 0 || callerID <= 0 || strings.TrimSpace(successURL) == "" {
		return StripePaymentLink{}, ErrInvalidInvoice
	}
	if s.stripeCredentials == nil || s.stripeGateway == nil {
		return StripePaymentLink{}, ErrStripeUnavailable
	}
	if s.authorizer == nil {
		return StripePaymentLink{}, ErrStripeUnavailable
	}
	role, member, err := s.authorizer.TeamMemberRole(ctx, appmodel.TeamMembershipQuery{TeamID: teamID, UserID: callerID})
	if err != nil {
		return StripePaymentLink{}, fmt.Errorf("authorize Stripe checkout: %w", err)
	}
	if !member || !role.CanManage() {
		return StripePaymentLink{}, model.ErrForbidden
	}
	details, err := s.reader.GetInvoiceDetails(ctx, appmodel.InvoiceLookupQuery{TeamID: teamID, InvoiceID: invoiceID})
	if err != nil {
		return StripePaymentLink{}, fmt.Errorf("load invoice for Stripe checkout: %w", err)
	}
	totalCents := 0
	for _, line := range details.Lines {
		totalCents, err = money.AddCents(totalCents, line.AmountCents)
		if err != nil {
			return StripePaymentLink{}, fmt.Errorf("sum invoice %d for Stripe checkout: %w", invoiceID, err)
		}
	}
	if totalCents <= 0 {
		return StripePaymentLink{}, ErrInvalidInvoice
	}
	key, _, err := s.stripeCredentials.TeamStripe(ctx, teamID)
	if err != nil {
		return StripePaymentLink{}, fmt.Errorf("load workspace Stripe credentials: %w", err)
	}
	if key == "" {
		key = s.stripeAPIKey
	}
	if key == "" {
		return StripePaymentLink{}, ErrStripeUnavailable
	}
	sessionID, paymentURL, err := s.stripeGateway.CreateCheckout(ctx, key, StripeCheckoutRequest{
		InvoiceID: invoiceID, TeamID: details.Invoice.TeamID, TotalCents: totalCents,
		Currency: details.Invoice.Currency, InvoiceNumber: details.Invoice.Number, SuccessURL: successURL,
	})
	if err != nil {
		return StripePaymentLink{}, fmt.Errorf("create Stripe checkout: %w", err)
	}
	if err := s.setPaymentLink(ctx, teamID, invoiceID, callerID, details.Invoice.Revision, paymentURL, sessionID); err != nil {
		cleanupCtx, cancel := postcommit.NewContextWithTimeout(ctx, 5*time.Second)
		defer cancel()
		if expireErr := s.stripeGateway.ExpireCheckout(cleanupCtx, key, sessionID); expireErr != nil {
			s.logger.Printf("invoicing: could not expire unpersisted Stripe checkout for team %d invoice %d session %q: %v", teamID, invoiceID, sessionID, expireErr)
		}
		return StripePaymentLink{}, err
	}
	s.recordAudit(ctx, teamID, callerID, "invoice.paylink", details.Invoice.Number, "stripe")
	return StripePaymentLink{URL: paymentURL, InvoiceNumber: details.Invoice.Number}, nil
}

// StripeReady reports whether a workspace or process-level API key is
// configured, without returning the key to the caller.
func (s *Service) StripeReady(ctx context.Context, teamID int64) (bool, error) {
	if teamID <= 0 {
		return false, ErrInvalidTeam
	}
	if s.stripeGateway == nil {
		return false, nil
	}
	if s.stripeCredentials == nil {
		return s.stripeAPIKey != "", nil
	}
	key, _, err := s.stripeCredentials.TeamStripe(ctx, teamID)
	if err != nil {
		if s.stripeAPIKey != "" {
			return true, nil
		}
		return false, fmt.Errorf("load workspace Stripe credentials: %w", err)
	}
	return key != "" || s.stripeAPIKey != "", nil
}

// ProcessStripeWebhook validates the provider signature, interprets the event
// and applies a confirmed payment through the invoice state transition.
func (s *Service) ProcessStripeWebhook(ctx context.Context, payment appmodel.StripePaymentEvent) (StripeWebhookResult, error) {
	if s.stripeGateway == nil {
		return StripeWebhookResult{}, ErrStripeUnavailable
	}
	event, err := s.stripeGateway.ParseWebhookEvent(payment.Body)
	if err != nil {
		return StripeWebhookResult{}, fmt.Errorf("%w: %w", ErrInvalidStripePayload, err)
	}
	teamID, teamIDErr := strconv.ParseInt(event.TeamID, 10, 64)
	secret := s.stripeWebhookSecret
	if teamIDErr == nil && teamID > 0 {
		if s.stripeCredentials == nil {
			return StripeWebhookResult{}, ErrStripeUnavailable
		}
		_, workspaceSecret, err := s.stripeCredentials.TeamStripe(ctx, teamID)
		if err != nil {
			return StripeWebhookResult{}, fmt.Errorf("load workspace Stripe credentials: %w", err)
		}
		if workspaceSecret != "" {
			secret = workspaceSecret
		}
	}
	if !s.stripeGateway.VerifyWebhookSignature(payment.Signature, payment.Body, secret, payment.ReceivedAt) {
		return StripeWebhookResult{}, ErrInvalidStripeSignature
	}

	supported, paid := false, false
	switch event.Type {
	case "checkout.session.completed":
		supported = true
		if event.PaymentStatus == "" {
			return StripeWebhookResult{}, ErrMissingStripePaymentStatus
		}
		paid = event.PaymentStatus == "paid" || event.PaymentStatus == "no_payment_required"
	case "checkout.session.async_payment_succeeded":
		supported, paid = true, true
	}
	if !supported || !paid {
		return StripeWebhookResult{}, nil
	}
	invoiceID, invoiceIDErr := strconv.ParseInt(event.InvoiceID, 10, 64)
	if teamIDErr != nil || teamID <= 0 || invoiceIDErr != nil || invoiceID <= 0 || event.SessionID == "" {
		return StripeWebhookResult{}, ErrInvalidStripeMetadata
	}
	changed, err := s.recordStripePayment(ctx, teamID, invoiceID, event.SessionID)
	if err != nil {
		return StripeWebhookResult{}, err
	}
	return StripeWebhookResult{TeamID: teamID, InvoiceID: invoiceID, Changed: changed}, nil
}

// SetPaymentLink stores a provider or manually configured payment URL. URL
// validation stays in the invoice workflow while paid-state protection is
// enforced transactionally by the store.
func (s *Service) setPaymentLink(ctx context.Context, teamID, invoiceID, callerID, expectedRevision int64, paymentURL, stripeSessionID string) error {
	if teamID <= 0 || invoiceID <= 0 || callerID <= 0 || expectedRevision <= 0 {
		return ErrInvalidInvoice
	}
	parsed, err := url.Parse(strings.TrimSpace(paymentURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ErrInvalidPaymentLink
	}
	if stripeSessionID != "" && strings.TrimSpace(stripeSessionID) == "" {
		return ErrInvalidInvoice
	}
	if err := s.writer.SetPaymentURL(ctx, appmodel.InvoicePaymentLinkSaveRequest{
		TeamID: teamID, InvoiceID: invoiceID, CallerID: callerID, ExpectedRevision: expectedRevision,
		PaymentURL: parsed.String(), StripeSessionID: stripeSessionID,
	}); err != nil {
		return fmt.Errorf("set invoice payment link: %w", err)
	}
	return nil
}

// SetManualPaymentLink stores a user-provided URL and records the audit event
// with the invoice number loaded inside the workflow.
func (s *Service) SetManualPaymentLink(ctx context.Context, request appmodel.InvoiceManualLinkRequest) error {
	if request.TeamID <= 0 {
		return ErrInvalidInvoice
	}
	teamID, invoiceID, callerID, paymentURL := request.TeamID, request.InvoiceID, request.CallerID, request.PaymentURL
	if teamID <= 0 || invoiceID <= 0 || callerID <= 0 {
		return ErrInvalidInvoice
	}
	details, err := s.Get(ctx, teamID, invoiceID)
	if err != nil {
		return err
	}
	if err := s.setPaymentLink(ctx, teamID, invoiceID, callerID, details.Invoice.Revision, paymentURL, ""); err != nil {
		return err
	}
	s.recordAudit(ctx, teamID, callerID, "invoice.paylink", details.Invoice.Number, "manual")
	return nil
}
