package web

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// ---------------------------------------------------------------------------
// Invoice documents and online payment (Stripe Checkout).
// ---------------------------------------------------------------------------

// handleInvoicePDF streams a downloadable PDF of the invoice.
func (s *Server) handleInvoicePDF(w http.ResponseWriter, r *http.Request) {
	inv, vm, err := s.loadInvoiceVM(r)
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
	inv, vm, err := s.loadInvoiceVM(r)
	if err != nil {
		s.invoiceLoadError(w, r, err)
		return
	}
	data := invoiceDetailPage{pageData: pageData{Title: inv.Number, Active: "invoices", Lang: string(resolveLang(r))}, Inv: vm}
	data.Seller = s.sellerName(r)
	s.renderPageForRequest(w, r, inv.Number, "invoices", "invoice-act", &data)
}

func (s *Server) handleInvoiceActPDF(w http.ResponseWriter, r *http.Request) {
	inv, vm, err := s.loadInvoiceVM(r)
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
		if errors.Is(err, model.ErrForbidden) {
			fail(s.invoiceErrorMessage(r, err))
		} else if errors.Is(err, appmodel.ErrStripeUnavailable) {
			fail("stripe key is not configured (team settings or process configuration)")
		} else if errors.Is(err, appmodel.ErrInvalidInvoice) {
			fail(s.invoiceErrorMessage(r, err))
		} else {
			s.logInternalError(err)
			fail(i18n.T(resolveLang(r), "err.internal"))
		}
		return
	}
	http.Redirect(w, r, "/invoices/"+strconv.FormatInt(invoiceID, 10)+"?flash="+encodeFlash(true, "payment link created"),
		http.StatusSeeOther)
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
