package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// ---------------------------------------------------------------------------
// Invoice documents and online payment (Stripe Checkout).
// ---------------------------------------------------------------------------

// handleInvoicePDF streams a downloadable PDF of the invoice.
// invoiceShareResponse is the link a manager just made: shown once, with its end.
type invoiceShareResponse struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
}

func (invoiceShareResponse) isJSONResponse() {}

// handleInvoiceShare makes a client's link to the invoice's PDF. The address is the only
// answer that carries the token; the manager sends it on (FIN-13).
func (s *Server) handleInvoiceShare(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: "not found"})
		return
	}
	now := userNow(r)
	token, expires, err := s.services.Invoicing.Shares.CreateShareLink(r.Context(), appmodel.InvoiceShareRequest{
		TeamID: teamID(r), InvoiceID: id, CallerID: authenticatedUserID(r), Now: now,
	})
	if err != nil {
		switch {
		case errors.Is(err, model.ErrNotFound):
			s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: "not found"})
		case errors.Is(err, model.ErrForbidden):
			s.writeJSONStatus(w, http.StatusForbidden, apiErrorResponse{Error: "forbidden"})
		case errors.Is(err, model.ErrInvoiceNotIssued):
			s.writeJSONStatus(w, http.StatusConflict, apiErrorResponse{Error: i18n.T(resolveLang(r), "inv.linkNotIssued")})
		case errors.As(err, new(*model.RequisitesError)):
			s.writeJSONStatus(w, http.StatusConflict, apiErrorResponse{Error: s.invoiceErrorMessage(r, err)})
		default:
			s.writeInternalJSONError(w, err)
		}
		return
	}
	s.audit(r, "invoice.share", strconv.FormatInt(id, 10), "")
	s.writeJSON(w, invoiceShareResponse{
		URL:       s.publicBaseURL(r) + "/share/invoices/" + token + ".pdf",
		ExpiresAt: fmtDate(resolveLang(r), expires.In(userLoc(r))),
	})
}

// handleSharedInvoicePDF serves an invoice to a client without an account, by the link a
// manager made (FIN-13). An unknown or expired token is not found, so the answer never
// says whether an invoice exists. Nothing from the page is cached.
func (s *Server) handleSharedInvoicePDF(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSuffix(r.PathValue("token"), ".pdf")
	link, err := s.services.Invoicing.Shares.ResolveShareLink(r.Context(), token, s.config.Now())
	if errors.Is(err, model.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	team, err := s.services.Teams.Directory.FindByID(r.Context(), link.TeamID)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	r = r.WithContext(WithTeam(r.Context(), team))
	_, vm, _, err := s.invoiceVMFor(r, link.InvoiceID, false)
	if err != nil {
		s.invoiceLoadError(w, r, err)
		return
	}
	// A voided invoice says so to the client instead of a bare "not found",
	// so nobody pays a cancelled number.
	if vm.Status == "void" {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusGone)
		msg := fmt.Sprintf(i18n.T(resolveLang(r), "inv.voidShared"), vm.Number)
		if vm.VoidReason != "" {
			msg += " " + vm.VoidReason
		}
		_, _ = w.Write([]byte(msg))
		return
	}
	pdf, err := renderInvoicePDF(vm, vm.Seller, resolveLang(r))
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+invoicePDFName(vm)+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	_, _ = w.Write(pdf)
}

// handleInvoicesExportCSV is the accountant's download: the lines of the issued invoices,
// one row each, for one client and a period of invoice dates in the viewer's zone (FIN-05).
func (s *Server) handleInvoicesExportCSV(w http.ResponseWriter, r *http.Request) {
	loc := userLoc(r)
	query := appmodel.InvoiceExportQuery{TeamID: teamID(r), Client: strings.TrimSpace(r.URL.Query().Get("export_client"))}
	query.ClientProjects = s.clientCardProjects(r, query.Client)
	valid := true
	if value := r.URL.Query().Get("export_from"); value != "" {
		day, err := time.ParseInLocation("2006-01-02", value, loc)
		query.From, valid = day, valid && err == nil
	}
	if value := r.URL.Query().Get("export_to"); value != "" {
		day, err := time.ParseInLocation("2006-01-02", value, loc)
		query.To, valid = day.AddDate(0, 0, 1), valid && err == nil
	}
	if !valid || (!query.From.IsZero() && !query.To.IsZero() && query.To.Before(query.From)) {
		http.Error(w, i18n.T(resolveLang(r), "export.invalidPeriod"), http.StatusBadRequest)
		return
	}
	rows, err := s.services.Invoicing.Queries.ExportLines(r.Context(), query)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	s.audit(r, "invoice.export", "", "")
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="invoices-export.csv"`)
	if err := writeInvoicesExportCSV(w, rows, s.sellerName(r), loc, dialectOf(r)); err != nil {
		s.logInternalError(err)
	}
}

// handleInvoiceSessionsCSV is the invoice's appendix: the sessions it bills, one row
// each, by day, in the viewer's zone (FIN-06). The page lists the invoice's lines summed;
// this is where the hours can be checked against the time they came from.
func (s *Server) handleInvoiceSessionsCSV(w http.ResponseWriter, r *http.Request) {
	inv, _, _, err := s.loadInvoiceVM(r, false)
	if err != nil {
		s.invoiceLoadError(w, r, err)
		return
	}
	body, err := s.invoiceSessionsCSV(r, inv.ID)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	s.audit(r, "invoice.sessions", inv.Number, "")
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="invoice-%d-sessions.csv"`, inv.ID))
	_, _ = w.Write(body)
}

// invoiceSessionsCSV is the invoice's appendix as a file: one row per billed
// session, people's names left out when the workspace hides them on invoices.
func (s *Server) invoiceSessionsCSV(r *http.Request, invoiceID int64) ([]byte, error) {
	rows, err := s.liveInvoiceSessions(r, invoiceID)
	if err != nil {
		return nil, err
	}
	// An issued invoice's appendix is the one the client was given.
	if raw, err := s.services.Invoicing.Queries.IssuedSnapshot(r.Context(), appmodel.InvoiceLookupQuery{TeamID: teamID(r), InvoiceID: invoiceID}); err == nil && raw != "" {
		var issued invoiceIssued
		if json.Unmarshal([]byte(raw), &issued) == nil && issued.Sessions != nil {
			rows = issued.Sessions
		}
	}
	var buf bytes.Buffer
	// The appendix goes to the client's accountant: the dialect a person opens.
	// Names are already left out of the rows when the invoice hides them.
	if err := writeInvoiceSessionsCSV(&buf, rows, userLoc(r), false, excelDialect(r)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// liveInvoiceSessions reads the billed sessions as they are now, with the
// people's names left out when the invoice hides them.
func (s *Server) liveInvoiceSessions(r *http.Request, invoiceID int64) ([]appmodel.InvoiceSessionRow, error) {
	rows, err := s.services.Invoicing.Queries.Sessions(r.Context(), appmodel.InvoiceLookupQuery{TeamID: teamID(r), InvoiceID: invoiceID})
	if err != nil {
		return nil, err
	}
	hide, err := s.invoiceHidesNames(r, model.Invoice{ID: invoiceID})
	if err != nil {
		return nil, err
	}
	if hide {
		for i := range rows {
			rows[i].Person = ""
		}
	}
	return rows, nil
}

func (s *Server) handleInvoicePDF(w http.ResponseWriter, r *http.Request) {
	inv, vm, _, err := s.loadInvoiceVM(r, false)
	if err != nil {
		s.invoiceLoadError(w, r, err)
		return
	}
	pdf, err := renderInvoicePDF(vm, vm.Seller, resolveLang(r))
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	s.audit(r, "invoice.pdf", inv.Number, "")
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+invoicePDFName(vm)+`"`)
	w.Write(pdf)
}

// handleInvoiceAct shows the certificate of completion for an invoice.
func (s *Server) handleInvoiceAct(w http.ResponseWriter, r *http.Request) {
	inv, vm, _, err := s.loadInvoiceVM(r, false)
	if err != nil {
		s.invoiceLoadError(w, r, err)
		return
	}
	data := invoiceDetailPage{pageData: pageData{Title: inv.Number, Active: "invoices", Lang: string(resolveLang(r))}, InvoiceActReact: true, Inv: vm}
	data.Seller = vm.Seller
	s.renderPageForRequest(w, r, inv.Number, "invoices", &data)
}

func (s *Server) handleInvoiceActPDF(w http.ResponseWriter, r *http.Request) {
	inv, vm, _, err := s.loadInvoiceVM(r, false)
	if err != nil {
		s.invoiceLoadError(w, r, err)
		return
	}
	pdf, err := renderDocPDF(vm, vm.Seller, resolveLang(r), true)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	s.audit(r, "invoice.act_pdf", inv.Number, "")
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="act-`+strings.ReplaceAll(inv.Number, " ", "-")+`.pdf"`)
	w.Write(pdf)
}

// handleInvoicePayLink creates a Stripe Checkout session (or accepts a
// manual payment URL) and stores it on the invoice.
//
// Form: mode=stripe|manual, url (for manual).
func (s *Server) handleInvoicePayLink(w http.ResponseWriter, r *http.Request) {
	invoiceID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || invoiceID <= 0 {
		s.invoiceLoadError(w, r, model.ErrNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(resolveLang(r), "err.invalidInput"), http.StatusBadRequest)
		return
	}
	mode := strings.TrimSpace(r.PostForm.Get("mode"))
	fail := func(msg string) {
		http.Redirect(w, r, "/invoices/"+strconv.FormatInt(invoiceID, 10)+"?flash="+encodeFlash(false, msg),
			http.StatusSeeOther)
	}
	if mode == "manual" {
		link := strings.TrimSpace(r.PostForm.Get("url"))
		if err := s.services.Invoicing.PaymentLinks.SetManualPaymentLink(operationContext(r), appmodel.InvoiceManualLinkRequest{TeamID: teamID(r), InvoiceID: invoiceID, CallerID: authenticatedUserID(r), PaymentURL: link}); err != nil {
			if errors.Is(err, appmodel.ErrInvalidPaymentLink) {
				fail("payment link must be an http(s) URL")
				return
			}
			fail(s.invoiceErrorMessage(r, err))
			return
		}
		http.Redirect(w, r, "/invoices/"+strconv.FormatInt(invoiceID, 10)+"?flash="+encodeFlash(true, "payment link saved"),
			http.StatusSeeOther)
		return
	}

	// Stripe Checkout
	_, err = s.services.Invoicing.PaymentLinks.CreateStripePaymentLink(operationContext(r), appmodel.InvoiceStripeLinkRequest{
		TeamID: teamID(r), InvoiceID: invoiceID, CallerID: authenticatedUserID(r),
		SuccessURL: s.publicBaseURL(r) + "/invoices/" + strconv.FormatInt(invoiceID, 10),
	})
	if err != nil {
		if errors.Is(err, model.ErrForbidden) || errors.Is(err, appmodel.ErrInvalidInvoice) {
			fail(s.invoiceErrorMessage(r, err))
		} else if reason, ok := stripeLinkReason(resolveLang(r), err); ok {
			s.logger.Printf("web: stripe payment link for invoice %d: %v", invoiceID, err)
			fail(reason)
		} else {
			s.logInternalError(err)
			fail(i18n.T(resolveLang(r), "err.internal"))
		}
		return
	}
	http.Redirect(w, r, "/invoices/"+strconv.FormatInt(invoiceID, 10)+"?flash="+encodeFlash(true, "payment link created"),
		http.StatusSeeOther)
}

// stripeLinkReason names the setting a refused Stripe link depends on. ok is false for
// an outage or any other failure: the owner cannot fix those in the settings, so they
// keep the general sentence and go to the log.
func stripeLinkReason(lang i18n.Lang, err error) (reason string, ok bool) {
	switch {
	case errors.Is(err, appmodel.ErrStripeKeyRejected):
		return i18n.T(lang, "stripe.keyRejected"), true
	case errors.Is(err, appmodel.ErrStripeUnavailable):
		return i18n.T(lang, "stripe.notConfigured"), true
	}
	return "", false
}

// handleStripeWebhook marks an invoice paid when Stripe says
// checkout.session.completed. Body is verified against the team's
// webhook secret when present (or STRIPE_WEBHOOK_SECRET).
func (s *Server) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad body", 400)
		return
	}
	_, err = s.services.Billing.ProcessStripeWebhook(r.Context(), appmodel.StripeWebhookRequest{
		Event: appmodel.StripePaymentEvent{
			Signature: r.Header.Get("Stripe-Signature"), Body: body, ReceivedAt: userNow(r),
		},
		ClientIP: clientIP(r),
	})
	if err != nil {
		if errors.Is(err, appmodel.ErrInvalidStripePayload) {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if errors.Is(err, appmodel.ErrInvalidStripeSignature) {
			http.Error(w, "bad signature", http.StatusBadRequest)
			return
		}
		if errors.Is(err, appmodel.ErrMissingStripePaymentStatus) {
			http.Error(w, "missing checkout payment status", http.StatusBadRequest)
			return
		}
		if errors.Is(err, appmodel.ErrInvalidStripeMetadata) {
			http.Error(w, "invalid payment metadata", http.StatusBadRequest)
			return
		}
		if errors.Is(err, model.ErrNotFound) {
			w.WriteHeader(200) // not ours to retry
			return
		}
		if errors.Is(err, appmodel.ErrStripeSessionMismatch) {
			w.WriteHeader(200) // stale or unrelated checkout session; retry cannot repair it
			return
		}
		if errors.Is(err, appmodel.ErrStripeSessionPending) {
			http.Error(w, "checkout session is not persisted yet", http.StatusServiceUnavailable)
			return // the provider should retry after payment-link persistence commits
		}
		s.writeInternalError(w, err) // Stripe retries
		return
	}
	w.WriteHeader(200)
}

// handleInvoiceMarkPaid is the manual "record payment" button.
func (s *Server) handleInvoiceMarkPaid(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if inv, vm, _, err := s.invoiceVMFor(r, id, false); err == nil && inv.Status == "draft" {
		if err := s.freezeIssued(r, inv, &vm); err != nil {
			s.logInternalError(err)
		}
	}
	if err := s.services.Billing.RecordManualPayment(r.Context(), appmodel.ManualPaymentRequest{
		TeamID: teamID(r), InvoiceID: id, CallerID: authenticatedUserID(r), ClientIP: clientIP(r),
	}); err != nil {
		http.Redirect(w, r, "/invoices/"+strconv.FormatInt(id, 10)+"?flash="+encodeFlash(false, s.invoiceErrorMessage(r, err)),
			http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/invoices/"+strconv.FormatInt(id, 10)+"?flash="+encodeFlash(true, "marked paid"),
		http.StatusSeeOther)
}

func (s *Server) invoiceLoadError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, model.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	s.writeInternalError(w, err)
}

// handlePaymentLedgerCSV is the accountant's ledger of received payments: one
// row per payment, by the day it was paid, for a period and optionally one
// client, in the same dialects as the invoice export (?format=excel for 1С).
func (s *Server) handlePaymentLedgerCSV(w http.ResponseWriter, r *http.Request) {
	loc := userLoc(r)
	query := appmodel.InvoiceExportQuery{TeamID: teamID(r), Client: strings.TrimSpace(r.URL.Query().Get("export_client"))}
	query.ClientProjects = s.clientCardProjects(r, query.Client)
	valid := true
	if value := r.URL.Query().Get("export_from"); value != "" {
		day, err := time.ParseInLocation("2006-01-02", value, loc)
		query.From, valid = day, valid && err == nil
	}
	if value := r.URL.Query().Get("export_to"); value != "" {
		day, err := time.ParseInLocation("2006-01-02", value, loc)
		query.To, valid = day.AddDate(0, 0, 1), valid && err == nil
	}
	if !valid || (!query.From.IsZero() && !query.To.IsZero() && query.To.Before(query.From)) {
		http.Error(w, i18n.T(resolveLang(r), "export.invalidPeriod"), http.StatusBadRequest)
		return
	}
	rows, err := s.services.Invoicing.Queries.PaymentLedger(r.Context(), query)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	s.audit(r, "invoice.payments_export", "", "")
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="payments.csv"`)
	if err := writePaymentLedgerCSV(w, rows, dialectOf(r)); err != nil {
		s.logInternalError(err)
	}
}

// invoiceShareRevokeResponse says how many client links were closed.
type invoiceShareRevokeResponse struct {
	Revoked int64 `json:"revoked"`
}

func (invoiceShareRevokeResponse) isJSONResponse() {}

// handleInvoiceShareRevoke closes every client link to the invoice now.
func (s *Server) handleInvoiceShareRevoke(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: "not found"})
		return
	}
	n, err := s.services.Invoicing.Shares.RevokeShareLinks(r.Context(), appmodel.InvoiceShareRevokeRequest{TeamID: teamID(r), InvoiceID: id, CallerID: authenticatedUserID(r)})
	if err != nil {
		switch {
		case errors.Is(err, model.ErrNotFound):
			s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: "not found"})
		case errors.Is(err, model.ErrForbidden):
			s.writeJSONStatus(w, http.StatusForbidden, apiErrorResponse{Error: "forbidden"})
		default:
			s.writeInternalJSONError(w, err)
		}
		return
	}
	s.audit(r, "invoice.share_revoke", strconv.FormatInt(id, 10), strconv.FormatInt(n, 10))
	s.writeJSON(w, invoiceShareRevokeResponse{Revoked: n})
}

// clientCardProjects lists the projects of the client card with this name,
// so an export for a client follows the card, as the reports do.
func (s *Server) clientCardProjects(r *http.Request, name string) []int64 {
	if name == "" {
		return nil
	}
	cards, err := s.services.Clients.List(r.Context(), appmodel.ClientCatalogQuery{TeamID: teamID(r), IncludeArchived: true})
	if err != nil {
		return nil
	}
	var cardID int64
	for _, c := range cards {
		if strings.EqualFold(strings.TrimSpace(c.Name), name) {
			cardID = c.ID
			break
		}
	}
	if cardID == 0 {
		return nil
	}
	projects, err := s.services.Projects.Queries.List(r.Context(), appmodel.ProjectCatalogQuery{TeamID: teamID(r), IncludeArchived: true})
	if err != nil {
		return nil
	}
	var ids []int64
	for _, p := range projects {
		if p.ClientID == cardID {
			ids = append(ids, p.ID)
		}
	}
	return ids
}
