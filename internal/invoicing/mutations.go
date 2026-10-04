package invoicing

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/postcommit"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// createDraft snapshots billable time and commits the invoice, lines, and
// billing stamps through one persistence operation.
func (s *Service) createDraft(ctx context.Context, request appmodel.InvoiceDraftRequest) (model.Invoice, []model.InvoiceLine, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return model.Invoice{}, nil, ErrInvalidTeam
	}
	if !request.End.After(request.Start) {
		return model.Invoice{}, nil, ErrInvalidPeriod
	}
	if request.Options.ProjectID < 0 {
		return model.Invoice{}, nil, ErrInvalidProject
	}
	request.Options.RememberClient = request.Options.RememberClient && request.Options.ProjectID > 0
	inv, lines, err := s.writer.CreateInvoiceDraft(ctx, request)
	if err != nil {
		return model.Invoice{}, nil, fmt.Errorf("create invoice draft: %w", err)
	}
	return inv, lines, nil
}

// CreateDraftWithOverlapCheck completes the invoice creation use case while
// preserving the non-blocking nature of duplicate-document warnings.
func (s *Service) CreateDraftWithOverlapCheck(ctx context.Context, request appmodel.InvoiceDraftRequest) (DraftCreation, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return DraftCreation{}, ErrInvalidTeam
	}
	invoice, lines, err := s.createDraft(ctx, request)
	if err != nil {
		return DraftCreation{}, err
	}
	s.recordAudit(ctx, request.TeamID, request.CallerID, "invoice.create", invoice.Number, invoice.ClientName)
	labels := make([]string, 0, len(lines))
	for _, line := range lines {
		labels = append(labels, line.Label)
	}
	overlaps, advisoryErr := s.overlappingDocuments(ctx, appmodel.InvoiceOverlapQuery{
		TeamID: request.TeamID, ExcludeInvoiceID: invoice.ID,
		Start: request.Start, End: request.End, Labels: labels,
	})
	return DraftCreation{
		Invoice: invoice, Lines: lines, Overlaps: overlaps, AdvisoryError: advisoryErr,
	}, nil
}

// MarkSent moves a draft invoice to sent once. Duplicate or stale requests
// cannot move an already paid invoice backwards.
func (s *Service) MarkSent(ctx context.Context, request appmodel.InvoiceMutationRequest) error {
	if request.TeamID <= 0 {
		return ErrInvalidInvoice
	}
	if request.InvoiceID <= 0 || request.CallerID <= 0 {
		return ErrInvalidInvoice
	}
	if err := s.writer.MarkInvoiceSentOnce(ctx, request); err != nil {
		return fmt.Errorf("mark invoice sent: %w", err)
	}
	return nil
}

// RecordPayment applies a manual payment once and returns the persisted invoice
// number for audit and response mapping.
func (s *Service) RecordPayment(ctx context.Context, request appmodel.ManualPaymentRequest) (string, bool, error) {
	if request.TeamID <= 0 || request.InvoiceID <= 0 || request.CallerID <= 0 {
		return "", false, ErrInvalidInvoice
	}
	number, changed, err := s.writer.MarkInvoicePaidOnce(ctx, appmodel.InvoiceMutationRequest{
		TeamID: request.TeamID, InvoiceID: request.InvoiceID, CallerID: request.CallerID,
	})
	if err != nil {
		return "", false, fmt.Errorf("record invoice payment: %w", err)
	}
	return number, changed, nil
}

// RecordStripePayment applies a provider payment only when its checkout
// session matches the session stored on the invoice.
func (s *Service) recordStripePayment(ctx context.Context, teamID, invoiceID int64, sessionID string) (bool, error) {
	if teamID <= 0 || invoiceID <= 0 || sessionID == "" {
		return false, ErrInvalidInvoice
	}
	changed, err := s.writer.MarkInvoicePaidFromStripe(ctx, appmodel.InvoiceStripePaymentRequest{
		TeamID: teamID, InvoiceID: invoiceID, StripeSessionID: sessionID,
	})
	if err != nil {
		return false, fmt.Errorf("record Stripe invoice payment: %w", err)
	}
	return changed, nil
}

// RebuildDraft refreshes an invoice's lines from current billable time. The
// store rechecks draft status and replaces lines and session stamps atomically.
func (s *Service) RebuildDraft(ctx context.Context, request appmodel.InvoiceMutationRequest) error {
	if request.TeamID <= 0 {
		return ErrInvalidInvoice
	}
	if request.InvoiceID <= 0 || request.CallerID <= 0 {
		return ErrInvalidInvoice
	}
	if err := s.writer.RebuildInvoice(ctx, request); err != nil {
		return fmt.Errorf("rebuild invoice draft: %w", err)
	}
	s.recordAudit(ctx, request.TeamID, request.CallerID, "invoice.rebuild", strconv.FormatInt(request.InvoiceID, 10), "")
	return nil
}

// UpdateDraftDetails changes editable invoice metadata and remembered
// project-client defaults as one store operation. The store rechecks draft
// status to protect against a concurrent send.
func (s *Service) UpdateDraftDetails(ctx context.Context, request appmodel.InvoiceDraftUpdateRequest) error {
	if request.TeamID <= 0 || request.InvoiceID <= 0 || request.CallerID <= 0 {
		return ErrInvalidInvoice
	}
	request.Client = strings.TrimSpace(request.Client)
	if request.Client == "" {
		return ErrInvalidClient
	}
	current, err := s.reader.GetInvoiceDetails(ctx, appmodel.InvoiceLookupQuery{TeamID: request.TeamID, InvoiceID: request.InvoiceID})
	if err != nil {
		return fmt.Errorf("load invoice for edit: %w", err)
	}
	if current.Invoice.Status != "draft" {
		return model.ErrInvoiceNotDraft
	}
	request.ProjectID = current.Invoice.ProjectID
	request.Details, request.Email, request.Notes = strings.TrimSpace(request.Details), strings.TrimSpace(request.Email), strings.TrimSpace(request.Notes)
	if err := s.writer.UpdateInvoiceMetaAndProjectClient(ctx, request); err != nil {
		return fmt.Errorf("update invoice details: %w", err)
	}
	s.recordAudit(ctx, request.TeamID, request.CallerID, "invoice.edit", current.Invoice.Number, "")
	return nil
}

// DeleteDraft delegates the state check and deletion to one locked
// persistence operation, so sent or paid documents cannot be removed.
func (s *Service) DeleteDraft(ctx context.Context, request appmodel.InvoiceMutationRequest) error {
	if request.TeamID <= 0 {
		return ErrInvalidInvoice
	}
	if request.InvoiceID <= 0 || request.CallerID <= 0 {
		return ErrInvalidInvoice
	}
	if err := s.writer.DeleteInvoice(ctx, request); err != nil {
		return fmt.Errorf("delete invoice draft: %w", err)
	}
	return nil
}

// CreateStripePaymentLink creates a provider checkout session and persists its
// URL and session ID on the invoice. Credentials and provider protocol details
// remain inside the invoicing workflow and its outbound ports.
func (s *Service) recordAudit(ctx context.Context, teamID, actorID int64, action, target, meta string) {
	effectCtx, cancel := postcommit.NewContext(ctx)
	defer cancel()
	if err := s.audit.Record(effectCtx, model.AuditRecord{
		TeamID: teamID, UserID: actorID, Action: action, Target: target, Meta: meta, IP: requestctx.ClientIP(ctx),
	}); err != nil {
		s.logger.Printf("invoicing: record %s audit for team %d: %v", action, teamID, err)
	}
}

// AssignHistoryToBillableProject moves an activity's entire unbilled history
// onto a billable project. The user must explicitly acknowledge that scope.
func (s *Service) AssignHistoryToBillableProject(ctx context.Context, request appmodel.AssignBillableHistoryRequest) error {
	if request.TeamID <= 0 {
		return ErrInvalidInvoice
	}
	teamID, activityID, projectID, callerID, confirmed := request.TeamID, request.ActivityID, request.ProjectID, request.CallerID, request.Confirmed
	if activityID <= 0 || projectID <= 0 || callerID <= 0 {
		return ErrInvalidInvoice
	}
	if !confirmed {
		return ErrHistoryConfirmationRequired
	}
	if err := s.writer.AssignUnassignedActivityForBilling(ctx, appmodel.AssignActivityProjectRequest{
		TeamID: teamID, ActivityID: activityID, ProjectID: projectID, CallerID: callerID,
	}); err != nil {
		return fmt.Errorf("assign activity history for billing: %w", err)
	}
	s.recordAudit(ctx, teamID, callerID, "activity.project", strconv.FormatInt(activityID, 10), strconv.FormatInt(projectID, 10))
	return nil
}

// SetReceipt updates the optional payment receipt attached to an invoice and
// advances its revision so prepared outbound documents can detect the edit.
func (s *Service) SetReceipt(ctx context.Context, request appmodel.InvoiceReceiptRequest) error {
	if request.TeamID <= 0 {
		return ErrInvalidInvoice
	}
	invoiceID, callerID, receipt := request.InvoiceID, request.CallerID, request.Receipt
	if invoiceID <= 0 || callerID <= 0 {
		return ErrInvalidInvoice
	}
	receipt = strings.TrimSpace(receipt)
	if len(receipt) > 300 {
		return ErrInvalidReceipt
	}
	request.Receipt = receipt
	if err := s.writer.SetInvoiceReceipt(ctx, request); err != nil {
		return fmt.Errorf("set invoice receipt: %w", err)
	}
	return nil
}
