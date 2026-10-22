package web

import (
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
	pdf, err := renderInvoicePDF(vm, s.sellerName(r), resolveLang(r))
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
	if err := writeInvoicesExportCSV(w, rows, s.sellerName(r), loc); err != nil {
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
	rows, err := s.services.Invoicing.Queries.Sessions(r.Context(), appmodel.InvoiceLookupQuery{TeamID: teamID(r), InvoiceID: inv.ID})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	s.audit(r, "invoice.sessions", inv.Number, "")
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="invoice-%d-sessions.csv"`, inv.ID))
	if err := writeInvoiceSessionsCSV(w, rows, userLoc(r)); err != nil {
		s.logInternalError(err)
	}
}

func (s *Server) handleInvoicePDF(w http.ResponseWriter, r *http.Request) {
	inv, vm, _, err := s.loadInvoiceVM(r, false)
	if err != nil {
		s.invoiceLoadError(w, r, err)
		return
	}
	teamName := s.sellerName(r)
	pdf, err := renderInvoicePDF(vm, teamName, resolveLang(r))
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
	data.Seller = s.sellerName(r)
	s.renderPageForRequest(w, r, inv.Number, "invoices", &data)
}

func (s *Server) handleInvoiceActPDF(w http.ResponseWriter, r *http.Request) {
	inv, vm, _, err := s.loadInvoiceVM(r, false)
	if err != nil {
		s.invoiceLoadError(w, r, err)
		return
	}
	teamName := s.sellerName(r)
	pdf, err := renderDocPDF(vm, teamName, resolveLang(r), true)
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
		http.Error(w, "invalid form", http.StatusBadRequest)
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
