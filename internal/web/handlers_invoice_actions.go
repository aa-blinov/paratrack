package web

import (
	"errors"
	"fmt"
	"net/http"
	netmail "net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/mailport"
)

func (s *Server) handleInvoiceAssignActivity(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	activityID, errA := strconv.ParseInt(r.PostForm.Get("activity_id"), 10, 64)
	projectID, errP := strconv.ParseInt(r.PostForm.Get("project_id"), 10, 64)
	if errA != nil || errP != nil || activityID <= 0 || projectID <= 0 {
		http.Error(w, i18n.T(resolveLang(r), "inv.assignInvalid"), http.StatusBadRequest)
		return
	}
	if err := s.services.Invoicing.Drafts.AssignHistoryToBillableProject(operationContext(r), appmodel.AssignBillableHistoryRequest{TeamID: teamID(r), ActivityID: activityID, ProjectID: projectID, CallerID: authenticatedUserID(r), Confirmed: r.PostForm.Get("confirm_history") == "1"}); errors.Is(err, appmodel.ErrHistoryConfirmationRequired) {
		http.Error(w, i18n.T(resolveLang(r), "inv.assignInvalid"), http.StatusBadRequest)
		return
	} else if err != nil {
		http.Redirect(w, r, "/invoices?flash="+url.QueryEscape(encodeFlash(false, i18n.T(resolveLang(r), "inv.assignFailed"))), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/invoices?project="+strconv.FormatInt(projectID, 10)+"&flash="+url.QueryEscape(encodeFlash(true, i18n.T(resolveLang(r), "inv.assignDone"))), http.StatusSeeOther)
}

// handleInvoiceCreate generates a draft from tracked time.

func (s *Server) handleInvoiceCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	client := strings.TrimSpace(r.PostForm.Get("client"))
	startStr := strings.TrimSpace(r.PostForm.Get("start"))
	endStr := strings.TrimSpace(r.PostForm.Get("end"))
	notes := strings.TrimSpace(r.PostForm.Get("notes"))
	projectStr := strings.TrimSpace(r.PostForm.Get("project_id"))
	start, err1 := time.ParseInLocation("2006-01-02", startStr, userLoc(r))
	end, err2 := time.ParseInLocation("2006-01-02", endStr, userLoc(r))
	if err1 != nil || err2 != nil || end.Before(start) {
		http.Redirect(w, r, "/invoices?flash="+encodeFlash(false, "bad period"), http.StatusSeeOther)
		return
	}
	// end is exclusive: make it the end of the chosen day
	end = end.AddDate(0, 0, 1)
	var projectID int64
	if projectStr != "" {
		parsedID, parseErr := strconv.ParseInt(projectStr, 10, 64)
		if parseErr != nil || parsedID <= 0 {
			http.Redirect(w, r, "/invoices?flash="+encodeFlash(false, "bad project"), http.StatusSeeOther)
			return
		}
		projectID = parsedID
	}
	byPerson := r.PostForm.Get("by_person") == "1"
	clientDetails := strings.TrimSpace(r.PostForm.Get("client_details"))
	clientEmail := strings.TrimSpace(r.PostForm.Get("client_email"))
	created, err := s.services.Invoicing.Drafts.CreateDraftWithOverlapCheck(operationContext(r), appmodel.InvoiceDraftRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), Client: client, Notes: notes,
		Start: start, End: end,
		Options: appmodel.InvoiceOptions{
			ClientDetails: clientDetails, ClientEmail: clientEmail,
			ProjectID: projectID, ByPerson: byPerson, RememberClient: projectID > 0,
		},
	})
	if errors.Is(err, appmodel.ErrNoBillableTime) {
		http.Redirect(w, r, "/invoices?flash="+encodeFlash(false, "no billable time in that period"), http.StatusSeeOther)
		return
	}
	if errors.Is(err, appmodel.ErrMixedCurrency) {
		http.Redirect(w, r, "/invoices?flash="+encodeFlash(false, "mixed currency"), http.StatusSeeOther)
		return
	}
	if errors.Is(err, appmodel.ErrAlreadyBilled) {
		http.Redirect(w, r, "/invoices?flash=already_billed", http.StatusSeeOther)
		return
	}
	if err != nil {
		http.Redirect(w, r, "/invoices?flash="+encodeFlash(false, s.invoiceErrorMessage(r, err)), http.StatusSeeOther)
		return
	}
	inv := created.Invoice
	if created.AdvisoryError != nil {
		s.logger.Printf("invoice %d created but overlapping-document check failed: %v", inv.ID, created.AdvisoryError)
	}
	if len(created.Overlaps) > 0 {
		msg := fmt.Sprintf(i18n.T(resolveLang(r), "inv.overlap"), strings.Join(created.Overlaps, ", "))
		http.Redirect(w, r, "/invoices/"+strconv.FormatInt(inv.ID, 10)+"?flash="+url.QueryEscape(encodeFlash(false, msg)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/invoices/"+strconv.FormatInt(inv.ID, 10), http.StatusSeeOther)
}

// handleInvoiceDetail renders one invoice (print-ready). Same view model
// as the PDF (loadInvoiceVM), so screen and file never disagree.

func (s *Server) handleInvoiceStatus(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	status := strings.TrimSpace(r.PostForm.Get("status"))
	if status != "sent" {
		http.Error(w, "invalid invoice status transition", http.StatusBadRequest)
		return
	}
	if err := s.services.Invoicing.Drafts.MarkSent(r.Context(), appmodel.InvoiceMutationRequest{TeamID: teamID(r), InvoiceID: id, CallerID: authenticatedUserID(r)}); err != nil {
		http.Redirect(w, r, "/invoices/"+strconv.FormatInt(id, 10)+"?flash="+encodeFlash(false, s.invoiceErrorMessage(r, err)),
			http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/invoices/"+strconv.FormatInt(id, 10)+"?flash="+encodeFlash(true, "updated"),
		http.StatusSeeOther)
}

// handleInvoiceDelete removes a draft.

func (s *Server) handleInvoiceDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.services.Invoicing.Drafts.DeleteDraft(r.Context(), appmodel.InvoiceMutationRequest{TeamID: teamID(r), InvoiceID: id, CallerID: authenticatedUserID(r)}); err != nil {
		http.Redirect(w, r, "/invoices?flash="+encodeFlash(false, s.invoiceErrorMessage(r, err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/invoices?flash="+encodeFlash(true, "removed"), http.StatusSeeOther)
}

// formatMoney renders cents as "12.34".
// formCents reads a money amount: "<name>" in currency units ("45.50",
// "45,5") from the UI, or "<name>_cents" as an integer from API clients.

func (s *Server) handleInvoiceEdit(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	err = s.services.Invoicing.Drafts.UpdateDraftDetails(r.Context(), appmodel.InvoiceDraftUpdateRequest{
		TeamID: teamID(r), InvoiceID: id, CallerID: authenticatedUserID(r),
		Client: f("client"), Details: f("client_details"), Email: f("client_email"), Notes: f("notes"),
	})
	if errors.Is(err, appmodel.ErrInvalidClient) {
		s.invoiceBack(w, r, false, i18n.T(resolveLang(r), "inv.clientRequired"))
		return
	}
	if errors.Is(err, appmodel.ErrInvoiceNotDraft) {
		s.invoiceBack(w, r, false, i18n.T(resolveLang(r), "inv.onlyDraft"))
		return
	}
	if err != nil {
		s.invoiceBack(w, r, false, s.invoiceErrorMessage(r, err))
		return
	}
	s.invoiceBack(w, r, true, "updated")
}

// handleInvoiceRebuild recomputes a draft's lines from the ledger.

func (s *Server) handleInvoiceRebuild(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	if err := s.services.Invoicing.Drafts.RebuildDraft(r.Context(), appmodel.InvoiceMutationRequest{TeamID: teamID(r), InvoiceID: id, CallerID: authenticatedUserID(r)}); err != nil {
		s.invoiceBack(w, r, false, s.invoiceErrorMessage(r, err))
		return
	}
	s.invoiceBack(w, r, true, i18n.T(resolveLang(r), "inv.rebuilt"))
}

// handleInvoiceReceipt stores the "Мой налог" receipt of a payment.

func (s *Server) handleInvoiceReceipt(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	rc := strings.TrimSpace(r.FormValue("receipt"))
	if err := s.services.Invoicing.Drafts.SetReceipt(r.Context(), appmodel.InvoiceReceiptRequest{TeamID: teamID(r), InvoiceID: id, CallerID: authenticatedUserID(r), Receipt: rc}); errors.Is(err, appmodel.ErrInvalidReceipt) {
		s.invoiceBack(w, r, false, "bad request")
		return
	} else if err != nil {
		s.invoiceBack(w, r, false, s.invoiceErrorMessage(r, err))
		return
	}
	s.invoiceBack(w, r, true, "updated")
}

// handleInvoiceSend freezes the rendered PDF in the durable queue. The queue
// moves the invoice to sending now and marks it sent only after delivery.

func (s *Server) handleInvoiceSend(w http.ResponseWriter, r *http.Request) {
	inv, vm, err := s.loadInvoiceVM(r)
	if err != nil {
		s.invoiceLoadError(w, r, err)
		return
	}
	if inv.Status != "draft" {
		s.invoiceBack(w, r, false, i18n.T(resolveLang(r), "inv.onlyDraft"))
		return
	}
	lang := resolveLang(r)
	to := strings.TrimSpace(r.FormValue("to"))
	if _, err := netmail.ParseAddress(to); err != nil {
		s.invoiceBack(w, r, false, i18n.T(lang, "inv.badEmail"))
		return
	}
	if !mailport.Available(s.runtime.Mailer) {
		s.invoiceBack(w, r, false, i18n.T(lang, "inv.mailOff"))
		return
	}
	seller := s.sellerName(r)
	pdf, err := renderInvoicePDF(vm, seller, lang)
	if err != nil {
		s.logInternalError(err)
		s.invoiceBack(w, r, false, i18n.T(lang, "err.internal"))
		return
	}
	subj, ev := invoiceEmail(lang, vm, seller)
	msg, err := s.buildEmail(to, subj, ev, mailport.Attachment{Name: invoicePDFName(vm), ContentType: "application/pdf", Data: pdf})
	if err != nil {
		s.logInternalError(err)
		s.invoiceBack(w, r, false, i18n.T(lang, "err.internal"))
		return
	}
	if err := s.services.MailQueue.EnqueueInvoice(operationContext(r), mailport.InvoiceQueueRequest{
		TeamID: teamID(r), InvoiceID: inv.ID, Revision: inv.Revision,
		InvoiceNumber: inv.Number, Recipient: to, Message: msg,
	}); err != nil {
		s.invoiceBack(w, r, false, s.invoiceErrorMessage(r, err))
		return
	}
	s.invoiceBack(w, r, true, fmt.Sprintf(i18n.T(lang, "inv.queued"), to))
}

// sellerName is who issues the documents: the workspace, or for a
// personal one ("Пространство: Аня") the owner's own name, which is what
// belongs on an invoice or act.
