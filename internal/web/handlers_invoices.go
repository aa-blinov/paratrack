package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/mailport"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
)

// ---------------------------------------------------------------------------
// Invoices
// ---------------------------------------------------------------------------

// handleInvoices lists invoices and offers a generator form.
func (s *Server) handleInvoices(w http.ResponseWriter, r *http.Request) {
	data, err := s.buildInvoicesPage(r)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	s.renderPageForRequest(w, r, "Invoices", "invoices", "invoices", &data)
}

// handleInvoiceDetail renders a print-ready invoice page using the same
// view model as its downloadable documents.
func (s *Server) handleInvoiceDetail(w http.ResponseWriter, r *http.Request) {
	inv, vm, err := s.loadInvoiceVM(r)
	if err != nil {
		s.invoiceLoadError(w, r, err)
		return
	}
	data := invoiceDetailPage{
		pageData: pageData{Title: inv.Number, Active: "invoices", Lang: string(resolveLang(r))},
		Inv:      vm,
	}
	data.Seller = s.sellerName(r)
	if ready, err := s.services.Invoicing.Queries.StripeReady(r.Context(), teamID(r)); err != nil {
		s.logInternalError(fmt.Errorf("load Stripe readiness for invoice page: %w", err))
	} else {
		data.StripeReady = ready
	}
	data.MailReady = mailport.Available(s.runtime.Mailer)
	subj, body := invoiceMailText(resolveLang(r), vm, data.Seller)
	data.MailtoURL = "mailto:" + url.PathEscape(vm.ClientEmail) + "?subject=" + url.QueryEscape(subj) + "&body=" + url.QueryEscape(body)
	data.MailtoURL = strings.ReplaceAll(data.MailtoURL, "+", "%20")
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, inv.Number, "invoices", "invoice-detail", &data)
}

// formCents parses a localized money value or an integer cents field.
func formCents(r *http.Request, name string) (cents int, has bool, err error) {
	bad := errors.New("bad amount")
	if v := strings.TrimSpace(r.Form.Get(name)); v != "" {
		// "2 500,50", "2500,50" and "2500.50" all mean the same amount.
		v = strings.NewReplacer(" ", "", "\u00a0", "", "\u202f", "", ",", ".").Replace(v)
		cents, err := money.ParseCents(v)
		if err != nil {
			return 0, true, bad
		}
		return cents, true, nil
	}
	if v := strings.TrimSpace(r.Form.Get(name + "_cents")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return 0, true, bad
		}
		return n, true, nil
	}
	return 0, false, nil
}

func formatMoney(cents int) string {
	return money.FormatCents(cents)
}

// formatMoneyL is the display form: "1 234,56" in Russian, "1,234.56" in
// English. formatMoney stays the plain parseable form for inputs and CSV.
func formatMoneyL(lang i18n.Lang, cents int) string {
	plain := formatMoney(cents)
	sign := ""
	if plain[0] == '-' {
		sign, plain = "-", plain[1:]
	}
	whole, frac := plain[:len(plain)-3], plain[len(plain)-2:]
	sep, dec := ",", "."
	if lang == i18n.Ru {
		sep, dec = "\u00a0", ","
	}
	var b strings.Builder
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteString(sep)
		}
		b.WriteRune(c)
	}
	return sign + b.String() + dec + frac
}

// invoiceMailText is the letter as plain text (the mailto draft uses it).
func invoiceMailText(lang i18n.Lang, vm invoiceVM, seller string) (string, string) {
	subj, ev := invoiceEmail(lang, vm, seller)
	return subj, emailText(ev)
}

// invoiceBack redirects to the invoice detail page with a result message.
func (s *Server) invoiceBack(w http.ResponseWriter, r *http.Request, ok bool, msg string) {
	http.Redirect(w, r, "/invoices/"+r.PathValue("id")+"?flash="+url.QueryEscape(encodeFlash(ok, msg)), http.StatusSeeOther)
}

// invoiceErrorMessage maps workflow and domain errors to user-facing text.
func (s *Server) invoiceErrorMessage(r *http.Request, err error) string {
	switch {
	case errors.Is(err, appmodel.ErrNoBillableTime):
		return "no billable time in that period"
	case errors.Is(err, appmodel.ErrMixedCurrency):
		return "invoice lines must use one currency"
	case errors.Is(err, appmodel.ErrAlreadyBilled):
		return "some time was already billed; reload and try again"
	case errors.Is(err, appmodel.ErrInvoiceNotDraft):
		return i18n.T(resolveLang(r), "inv.onlyDraft")
	case errors.Is(err, appmodel.ErrInvalidClient), errors.Is(err, appmodel.ErrInvalidPaymentLink),
		errors.Is(err, appmodel.ErrInvalidReceipt), errors.Is(err, appmodel.ErrInvalidInvoice),
		errors.Is(err, appmodel.ErrHistoryConfirmationRequired):
		return i18n.T(resolveLang(r), "err.invalidInput")
	case errors.Is(err, model.ErrNotFound):
		return "invoice not found"
	case errors.Is(err, model.ErrForbidden):
		return "manager role required"
	case errors.Is(err, model.ErrInvoiceChanged):
		return "invoice changed while the email was being prepared; reload and try again"
	default:
		s.logInternalError(err)
		return i18n.T(resolveLang(r), "err.internal")
	}
}
