package web

import (
	"errors"
	"fmt"
	"net/http"
	netmail "net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/mailport"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
)

func (s *Server) handleInvoiceAssignActivity(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(resolveLang(r), "err.invalidInput"), http.StatusBadRequest)
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
		http.Redirect(w, r, "/invoices?flash="+encodeFlash(false, i18n.T(resolveLang(r), "inv.assignFailed")), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/invoices?project="+strconv.FormatInt(projectID, 10)+"&flash="+encodeFlash(true, i18n.T(resolveLang(r), "inv.assignDone")), http.StatusSeeOther)
}

// handleInvoiceCreate generates a draft from tracked time.

func (s *Server) handleInvoiceCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(resolveLang(r), "err.invalidInput"), http.StatusBadRequest)
		return
	}
	client := strings.TrimSpace(r.PostForm.Get("client"))
	startStr := strings.TrimSpace(r.PostForm.Get("start"))
	endStr := strings.TrimSpace(r.PostForm.Get("end"))
	notes := strings.TrimSpace(r.PostForm.Get("notes"))
	projectStr := strings.TrimSpace(r.PostForm.Get("project_id"))
	start, end, ok := invoicePeriod(r, startStr, endStr)
	if !ok {
		http.Redirect(w, r, "/invoices?flash="+encodeFlash(false, i18n.T(resolveLang(r), "flash.msg.bad period")), http.StatusSeeOther)
		return
	}
	var projectID int64
	if projectStr != "" {
		parsedID, parseErr := strconv.ParseInt(projectStr, 10, 64)
		if parseErr != nil || parsedID <= 0 {
			http.Redirect(w, r, "/invoices?flash="+encodeFlash(false, i18n.T(resolveLang(r), "flash.msg.bad project")), http.StatusSeeOther)
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
			UpdateClientCard: r.PostForm.Get("update_client_card") != "",
		},
		ConfirmUnapproved: r.PostForm.Get("confirm_unapproved") == "1" || !s.teamModules(r)["payroll"],
	})
	var billed *model.AlreadyBilledInPeriodError
	if errors.As(err, &billed) && len(billed.Invoices) > 0 {
		// The period is not empty; its time is on named documents. Saying so
		// replaces a misleading "nothing to bill" that reads as "wrong dates".
		msg := fmt.Sprintf(i18n.T(resolveLang(r), "inv.billedInPeriod"), strings.Join(billed.Invoices, ", "))
		http.Redirect(w, r, "/invoices?flash="+encodeFlash(false, msg), http.StatusSeeOther)
		return
	}
	if errors.Is(err, appmodel.ErrNoBillableTime) {
		http.Redirect(w, r, "/invoices?flash="+encodeFlash(false, i18n.T(resolveLang(r), "flash.msg.no billable time in that period")), http.StatusSeeOther)
		return
	}
	var unapproved *model.UnapprovedWeeksError
	if errors.As(err, &unapproved) {
		msg := fmt.Sprintf(i18n.T(resolveLang(r), "inv.unapprovedFlash"), strings.Join(unapprovedWeekLabels(resolveLang(r), unapproved.Weeks), ", "))
		http.Redirect(w, r, "/invoices?flash="+encodeFlash(false, msg), http.StatusSeeOther)
		return
	}
	if errors.Is(err, appmodel.ErrMixedCurrency) {
		http.Redirect(w, r, "/invoices?flash="+encodeFlash(false, i18n.T(resolveLang(r), "flash.msg.mixed currency")), http.StatusSeeOther)
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
		http.Redirect(w, r, "/invoices/"+strconv.FormatInt(inv.ID, 10)+"?flash="+encodeFlash(false, msg), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/invoices/"+strconv.FormatInt(inv.ID, 10), http.StatusSeeOther)
}

// handleInvoiceDetail renders one invoice (print-ready). Same view model
// as the PDF (loadInvoiceVM), so screen and file never disagree.

func (s *Server) handleInvoiceStatus(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(resolveLang(r), "err.invalidInput"), http.StatusBadRequest)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	status := strings.TrimSpace(r.PostForm.Get("status"))
	if status != "sent" {
		http.Error(w, i18n.T(resolveLang(r), "err.invoiceStatusTransition"), http.StatusBadRequest)
		return
	}
	// The look the client gets is kept before the invoice leaves the draft.
	if inv, vm, _, err := s.invoiceVMFor(r, id, false); err == nil && inv.Status == "draft" {
		if err := s.freezeIssuedUnlessKept(r, inv, &vm); err != nil {
			s.logInternalError(err)
		}
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
	http.Redirect(w, r, "/invoices?flash="+encodeFlash(true, "invoice_deleted"), http.StatusSeeOther)
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
	if email := f("client_email"); email != "" {
		if _, err := netmail.ParseAddress(email); err != nil {
			s.invoiceBack(w, r, false, i18n.T(resolveLang(r), "clients.invalidEmail"))
			return
		}
	}
	err = s.services.Invoicing.Drafts.UpdateDraftDetails(r.Context(), appmodel.InvoiceDraftUpdateRequest{
		TeamID: teamID(r), InvoiceID: id, CallerID: authenticatedUserID(r),
		Client: f("client"), Details: f("client_details"), Email: f("client_email"), Notes: f("notes"), DueDate: f("due"),
		UpdateClientCard: r.FormValue("update_client_card") != "",
	})
	if errors.Is(err, appmodel.ErrInvalidClient) {
		s.invoiceBack(w, r, false, i18n.T(resolveLang(r), "inv.clientRequired"))
		return
	}
	if errors.Is(err, appmodel.ErrInvalidDue) {
		s.invoiceBack(w, r, false, i18n.T(resolveLang(r), "inv.dueInvalid"))
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
	inv, vm, _, err := s.loadInvoiceVM(r, false)
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
	if err := s.freezeIssued(r, inv, &vm); err != nil {
		s.logInternalError(err)
		s.invoiceBack(w, r, false, i18n.T(lang, "err.internal"))
		return
	}
	seller := vm.Seller
	pdf, err := renderInvoicePDF(vm, seller, lang)
	if err != nil {
		s.logInternalError(err)
		s.invoiceBack(w, r, false, i18n.T(lang, "err.internal"))
		return
	}
	// The client gets what they need to agree the invoice in one letter: the
	// invoice, the act for the same lines, and the sessions behind the hours.
	// Some clients sign the act only after paying, so it can stay out.
	withAct := r.FormValue("no_act") == ""
	var act []byte
	if withAct {
		act, err = renderDocPDF(vm, seller, lang, true)
		if err != nil {
			s.logInternalError(err)
			s.invoiceBack(w, r, false, i18n.T(lang, "err.internal"))
			return
		}
	}
	sessions, err := s.invoiceSessionsCSV(r, inv.ID)
	if err != nil {
		s.logInternalError(err)
		s.invoiceBack(w, r, false, i18n.T(lang, "err.internal"))
		return
	}
	subj, ev := invoiceEmail(lang, vm, seller)
	number := strings.ReplaceAll(vm.Number, " ", "-")
	files := []mailport.Attachment{{Name: invoicePDFName(vm), ContentType: "application/pdf", Data: pdf}}
	if withAct {
		files = append(files, mailport.Attachment{Name: "act-" + number + ".pdf", ContentType: "application/pdf", Data: act})
	}
	files = append(files, mailport.Attachment{Name: number + "-sessions.csv", ContentType: "text/csv; charset=utf-8", Data: sessions})
	msg, err := s.buildEmail(r, to, subj, ev, files...)
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

// unapprovedWeeksResponse carries the weeks the invoice form asks the manager
// about before it submits (FIN-12).
type unapprovedWeeksResponse struct {
	Weeks []string `json:"weeks"`
}

func (unapprovedWeeksResponse) isJSONResponse() {}

// handleInvoiceUnapproved names the weeks of a period that still carry unapproved
// billable time, so the invoice form asks the manager before it submits (FIN-12).
func (s *Server) handleInvoiceUnapproved(w http.ResponseWriter, r *http.Request) {
	if !s.teamModules(r)["payroll"] {
		s.writeJSON(w, unapprovedWeeksResponse{Weeks: []string{}})
		return
	}
	start, end, ok := invoicePeriod(r, r.URL.Query().Get("start"), r.URL.Query().Get("end"))
	if !ok {
		http.Error(w, i18n.T(resolveLang(r), "err.badDate"), http.StatusBadRequest)
		return
	}
	projectID, _ := strconv.ParseInt(r.URL.Query().Get("project_id"), 10, 64)
	weeks, err := s.services.Invoicing.Queries.UnapprovedWeeks(r.Context(), teamID(r), projectID, start, end)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	s.writeJSON(w, unapprovedWeeksResponse{Weeks: unapprovedWeekLabels(resolveLang(r), weeks)})
}

// invoicePeriod reads a day-based period: both days are inclusive in the form,
// so the returned end is the day after the last one (exclusive).
func invoicePeriod(r *http.Request, from, to string) (time.Time, time.Time, bool) {
	start, err1 := time.ParseInLocation("2006-01-02", from, userLoc(r))
	end, err2 := time.ParseInLocation("2006-01-02", to, userLoc(r))
	if err1 != nil || err2 != nil || end.Before(start) {
		return time.Time{}, time.Time{}, false
	}
	return start, end.AddDate(0, 0, 1), true
}

// unapprovedWeekLabels prints each week by its first and last day, the way the
// rest of the invoice prints its dates.
func unapprovedWeekLabels(lang i18n.Lang, weeks []time.Time) []string {
	labels := make([]string, 0, len(weeks))
	for _, monday := range weeks {
		labels = append(labels, periodSpan(lang, fmtDay(lang, monday), fmtDay(lang, monday.AddDate(0, 0, 6))))
	}
	return labels
}

// handleInvoiceLetter hands the invoice letter, PDF attached, to the sender's own
// mail program (FIN-10). It is the way out when this server has no SMTP host: a
// mailto link cannot carry the file.
func (s *Server) handleInvoiceLetter(w http.ResponseWriter, r *http.Request) {
	inv, vm, _, err := s.loadInvoiceVM(r, false)
	if err != nil {
		s.invoiceLoadError(w, r, err)
		return
	}
	// The letter file is a way of issuing the invoice like the send: the look
	// is kept as it goes into the letter, so marking it sent afterwards keeps
	// the same day, and the letter carries the act and the breakdown too.
	if inv.Status == "draft" {
		if err := s.freezeIssued(r, inv, &vm); err != nil {
			s.writeInternalError(w, err)
			return
		}
	}
	lang := resolveLang(r)
	seller := vm.Seller
	pdf, err := renderInvoicePDF(vm, seller, lang)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	act, err := renderDocPDF(vm, seller, lang, true)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	sessions, err := s.invoiceSessionsCSV(r, inv.ID)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	number := strings.ReplaceAll(vm.Number, " ", "-")
	subj, ev := invoiceEmail(lang, vm, seller)
	msg, err := s.buildEmail(r, vm.ClientEmail, subj, ev,
		mailport.Attachment{Name: invoicePDFName(vm), ContentType: "application/pdf", Data: pdf},
		mailport.Attachment{Name: "act-" + number + ".pdf", ContentType: "application/pdf", Data: act},
		mailport.Attachment{Name: number + "-sessions.csv", ContentType: "text/csv; charset=utf-8", Data: sessions})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	from := "paratrack@localhost"
	if u, ok := UserFrom(r.Context()); ok && u.Email != "" {
		from = u.Email
	}
	renderer, ok := s.runtime.Mailer.(mailport.EMLRenderer)
	if !ok {
		s.writeInternalError(w, errors.New("mail sender cannot render a letter"))
		return
	}
	raw, err := renderer.EML(from, msg)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "message/rfc822")
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.TrimSuffix(invoicePDFName(vm), ".pdf")+`.eml"`)
	_, _ = w.Write(raw)
}

// handleInvoicePayment books a payment into the invoice's journal (FIN-08). The
// amount is typed as money ("1 500,50"); the store refuses more than is still owed.
func (s *Server) handleInvoicePayment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(resolveLang(r), "err.invalidInput"), http.StatusBadRequest)
		return
	}
	lang := resolveLang(r)
	cents, err := money.ParseCents(moneyTyped(r.PostForm.Get("amount")))
	if err != nil || cents <= 0 {
		s.invoiceBack(w, r, false, i18n.T(lang, "inv.paymentAmountBad"))
		return
	}
	settled, err := s.services.Invoicing.Drafts.BookPayment(r.Context(), appmodel.InvoicePaymentRequest{
		TeamID: teamID(r), InvoiceID: id, CallerID: authenticatedUserID(r),
		PaidOn: strings.TrimSpace(r.PostForm.Get("paid_on")), AmountCents: cents, Note: r.PostForm.Get("note"),
	})
	switch {
	case errors.Is(err, model.ErrPaymentAmount):
		s.invoiceBack(w, r, false, i18n.T(lang, "inv.paymentTooMuch"))
	case errors.Is(err, model.ErrPaymentDate):
		s.invoiceBack(w, r, false, i18n.T(lang, "inv.paymentDateBad"))
	case errors.Is(err, model.ErrInvoiceNotPayable):
		s.invoiceBack(w, r, false, i18n.T(lang, "inv.paymentNotSent"))
	case err != nil:
		s.invoiceBack(w, r, false, s.invoiceErrorMessage(r, err))
	case settled:
		s.invoiceBack(w, r, true, i18n.T(lang, "inv.paymentSettled"))
	default:
		s.invoiceBack(w, r, true, i18n.T(lang, "inv.paymentSaved"))
	}
}

// handleInvoiceResend sends an issued invoice again, as it went out: the
// letter was lost or went to an old address. The invoice's status does not
// move; the letter goes through the same durable queue. Form: to, no_act.
func (s *Server) handleInvoiceResend(w http.ResponseWriter, r *http.Request) {
	inv, vm, _, err := s.loadInvoiceVM(r, false)
	if err != nil {
		s.invoiceLoadError(w, r, err)
		return
	}
	lang := resolveLang(r)
	if inv.Status != "sent" && inv.Status != "paid" {
		s.invoiceBack(w, r, false, i18n.T(lang, "inv.resendIssuedOnly"))
		return
	}
	to := strings.TrimSpace(r.FormValue("to"))
	if _, err := netmail.ParseAddress(to); err != nil {
		s.invoiceBack(w, r, false, i18n.T(lang, "inv.badEmail"))
		return
	}
	if !mailport.Available(s.runtime.Mailer) {
		s.invoiceBack(w, r, false, i18n.T(lang, "inv.mailOff"))
		return
	}
	seller := vm.Seller
	pdf, err := renderInvoicePDF(vm, seller, lang)
	if err != nil {
		s.logInternalError(err)
		s.invoiceBack(w, r, false, i18n.T(lang, "err.internal"))
		return
	}
	number := strings.ReplaceAll(vm.Number, " ", "-")
	files := []mailport.Attachment{{Name: invoicePDFName(vm), ContentType: "application/pdf", Data: pdf}}
	if r.FormValue("no_act") == "" {
		act, err := renderDocPDF(vm, seller, lang, true)
		if err != nil {
			s.logInternalError(err)
			s.invoiceBack(w, r, false, i18n.T(lang, "err.internal"))
			return
		}
		files = append(files, mailport.Attachment{Name: "act-" + number + ".pdf", ContentType: "application/pdf", Data: act})
	}
	sessions, err := s.invoiceSessionsCSV(r, inv.ID)
	if err != nil {
		s.logInternalError(err)
		s.invoiceBack(w, r, false, i18n.T(lang, "err.internal"))
		return
	}
	files = append(files, mailport.Attachment{Name: number + "-sessions.csv", ContentType: "text/csv; charset=utf-8", Data: sessions})
	subj, ev := invoiceEmail(lang, vm, seller)
	msg, err := s.buildEmail(r, to, subj, ev, files...)
	if err != nil {
		s.logInternalError(err)
		s.invoiceBack(w, r, false, i18n.T(lang, "err.internal"))
		return
	}
	if err := s.services.MailQueue.EnqueueReminder(operationContext(r), mailport.ReminderQueueRequest{TeamID: teamID(r), Recipient: to, Message: msg}); err != nil {
		s.invoiceBack(w, r, false, s.invoiceErrorMessage(r, err))
		return
	}
	s.audit(r, "invoice.resend", inv.Number, to)
	s.invoiceBack(w, r, true, i18n.T(lang, "inv.resendQueued"))
}

// handleInvoiceVoid cancels a sent invoice nothing was paid on. Form: reason.
func (s *Server) handleInvoiceVoid(w http.ResponseWriter, r *http.Request) {
	lang := resolveLang(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(lang, "err.invalidInput"), http.StatusBadRequest)
		return
	}
	err = s.services.Invoicing.Drafts.Void(r.Context(), appmodel.InvoiceVoidRequest{
		TeamID: teamID(r), InvoiceID: id, CallerID: authenticatedUserID(r), Reason: r.PostForm.Get("reason"),
	})
	var closedMonth *model.PeriodClosedError
	switch {
	case errors.Is(err, model.ErrPaymentReason):
		s.invoiceBack(w, r, false, i18n.T(lang, "inv.voidNeedsReason"))
	case errors.Is(err, model.ErrInvoiceNotVoidable):
		s.invoiceBack(w, r, false, i18n.T(lang, "inv.voidNotAllowed"))
	case errors.As(err, &closedMonth):
		s.invoiceBack(w, r, false, monthClosedMessage(lang, closedMonth))
	case err != nil:
		s.invoiceBack(w, r, false, s.invoiceErrorMessage(r, err))
	default:
		s.invoiceBack(w, r, true, i18n.T(lang, "inv.voidDone"))
	}
}

// handleInvoicePaymentReverse books a storno of one journal entry. Form: note.
func (s *Server) handleInvoicePaymentReverse(w http.ResponseWriter, r *http.Request) {
	lang := resolveLang(r)
	id, err1 := strconv.ParseInt(r.PathValue("id"), 10, 64)
	paymentID, err2 := strconv.ParseInt(r.PathValue("payment"), 10, 64)
	if err1 != nil || err2 != nil || id <= 0 || paymentID <= 0 {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(lang, "err.invalidInput"), http.StatusBadRequest)
		return
	}
	err := s.services.Invoicing.Drafts.ReversePayment(r.Context(), appmodel.InvoicePaymentReverseRequest{
		TeamID: teamID(r), InvoiceID: id, PaymentID: paymentID, CallerID: authenticatedUserID(r), Note: r.PostForm.Get("note"),
	})
	var closedMonth *model.PeriodClosedError
	switch {
	case errors.Is(err, model.ErrPaymentReason):
		s.invoiceBack(w, r, false, i18n.T(lang, "inv.reverseNeedsReason"))
	case errors.Is(err, model.ErrPaymentAmount):
		s.invoiceBack(w, r, false, i18n.T(lang, "inv.reverseNotAllowed"))
	case errors.As(err, &closedMonth):
		s.invoiceBack(w, r, false, monthClosedMessage(lang, closedMonth))
	case err != nil:
		s.invoiceBack(w, r, false, s.invoiceErrorMessage(r, err))
	default:
		s.invoiceBack(w, r, true, i18n.T(lang, "inv.reverseDone"))
	}
}

// moneyTyped reads an amount the way a person types it: spaces between the digits
// (including the non-breaking ones) and a decimal comma are both fine, "1 500,50".
func moneyTyped(value string) string {
	return strings.NewReplacer(" ", "", "\u00a0", "", "\u202f", "", ",", ".").Replace(value)
}

func (s *Server) handleInvoiceAddLine(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.invoiceBack(w, r, false, i18n.T(resolveLang(r), "flash.msg.invalid form"))
		return
	}
	label := strings.TrimSpace(r.PostForm.Get("label"))
	cents, err := parseSignedCents(r.PostForm.Get("amount"))
	if err != nil || label == "" {
		s.invoiceBack(w, r, false, i18n.T(resolveLang(r), "inv.lineBad"))
		return
	}
	if err := s.services.Invoicing.Drafts.AddDraftLine(r.Context(), appmodel.InvoiceLineAddRequest{
		TeamID: teamID(r), InvoiceID: id, CallerID: authenticatedUserID(r), Label: label, AmountCents: cents,
	}); err != nil {
		s.invoiceBack(w, r, false, s.invoiceErrorMessage(r, err))
		return
	}
	http.Redirect(w, r, "/invoices/"+strconv.FormatInt(id, 10)+"?flash="+encodeFlash(true, i18n.T(resolveLang(r), "inv.lineAdded")), http.StatusSeeOther)
}

func (s *Server) handleInvoiceDeleteLine(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	lineID, err := strconv.ParseInt(r.PathValue("lineId"), 10, 64)
	if err != nil || lineID <= 0 {
		http.NotFound(w, r)
		return
	}
	if err := s.services.Invoicing.Drafts.DeleteDraftLine(r.Context(), appmodel.InvoiceLineDeleteRequest{
		TeamID: teamID(r), InvoiceID: id, LineID: lineID, CallerID: authenticatedUserID(r),
	}); err != nil {
		s.invoiceBack(w, r, false, s.invoiceErrorMessage(r, err))
		return
	}
	http.Redirect(w, r, "/invoices/"+strconv.FormatInt(id, 10)+"?flash="+encodeFlash(true, i18n.T(resolveLang(r), "inv.lineDeleted")), http.StatusSeeOther)
}
