package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/db"
)

// ---------------------------------------------------------------------------
// Wave 5: PDF export + online payment (Stripe Checkout)
// ---------------------------------------------------------------------------

// handleInvoicePDF streams a downloadable PDF of the invoice.
func (s *Server) handleInvoicePDF(w http.ResponseWriter, r *http.Request) {
	inv, lines, vm, ok := s.loadInvoiceVM(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	teamName := "paratrack"
	if t, ok := TeamFrom(r.Context()); ok {
		teamName = t.Name
	}
	pdf, err := renderInvoicePDF(vm, teamName, resolveLang(r))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.audit(r, "invoice.pdf", inv.Number, "")
	_ = lines
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+invoicePDFName(vm)+`"`)
	w.Write(pdf)
}

// handleInvoiceAct shows the certificate of completion for an invoice.
func (s *Server) handleInvoiceAct(w http.ResponseWriter, r *http.Request) {
	inv, _, vm, ok := s.loadInvoiceVM(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	data := invoiceDetailPage{pageData: pageData{Title: inv.Number, Active: "invoices", Lang: string(resolveLang(r))}, Inv: vm}
	if t, ok := TeamFrom(r.Context()); ok {
		data.Seller = t.Name
	}
	s.renderPageForRequest(w, r, inv.Number, "invoices", "invoice-act", &data)
}

func (s *Server) handleInvoiceActPDF(w http.ResponseWriter, r *http.Request) {
	inv, _, vm, ok := s.loadInvoiceVM(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	teamName := "paratrack"
	if t, ok := TeamFrom(r.Context()); ok {
		teamName = t.Name
	}
	pdf, err := renderDocPDF(vm, teamName, resolveLang(r), true)
	if err != nil {
		http.Error(w, err.Error(), 500)
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
	inv, _, vm, ok := s.loadInvoiceVM(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()
	mode := strings.TrimSpace(r.PostForm.Get("mode"))
	fail := func(msg string) {
		http.Redirect(w, r, "/invoices/"+strconv.FormatInt(inv.ID, 10)+"?flash="+encodeFlash(false, msg),
			http.StatusSeeOther)
	}
	if mode == "manual" {
		link := strings.TrimSpace(r.PostForm.Get("url"))
		if link == "" || (!strings.HasPrefix(link, "https://") && !strings.HasPrefix(link, "http://")) {
			fail("payment link must be an http(s) URL")
			return
		}
		if err := s.db.SetPaymentURL(r.Context(), teamID(r), inv.ID, link, ""); err != nil {
			fail(err.Error())
			return
		}
		s.audit(r, "invoice.paylink", inv.Number, "manual")
		http.Redirect(w, r, "/invoices/"+strconv.FormatInt(inv.ID, 10)+"?flash="+encodeFlash(true, "payment link saved"),
			http.StatusSeeOther)
		return
	}

	// Stripe Checkout
	key, _, err := s.db.TeamStripe(r.Context(), teamID(r))
	if err != nil || key == "" {
		if env := stripeKeyFromEnv(); env != "" {
			key = env
		} else {
			fail("stripe key is not configured (team settings or PARATRACK_STRIPE_KEY)")
			return
		}
	}
	sessionURL, sessionID, err := createStripeCheckout(key, vm, publicBaseURL(r)+"/invoices/"+strconv.FormatInt(inv.ID, 10))
	if err != nil {
		fail(err.Error())
		return
	}
	if err := s.db.SetPaymentURL(r.Context(), teamID(r), inv.ID, sessionURL, sessionID); err != nil {
		fail(err.Error())
		return
	}
	s.audit(r, "invoice.paylink", inv.Number, "stripe")
	s.fireWebhook(r, "invoice.payment_link_created", map[string]any{
		"invoice_id": inv.ID, "number": inv.Number, "payment_url": sessionURL,
	})
	http.Redirect(w, r, "/invoices/"+strconv.FormatInt(inv.ID, 10)+"?flash="+encodeFlash(true, "payment link created"),
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
	var payload struct {
		Type string `json:"type"`
		Data struct {
			Object struct {
				ID       string `json:"id"`
				Metadata struct {
					InvoiceID string `json:"invoice_id"`
					TeamID    string `json:"team_id"`
				} `json:"metadata"`
				PaymentStatus string `json:"payment_status"`
			} `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "bad json", 400)
		return
	}
	// team_id is untrusted until the signature checks out: it only picks
	// which secret to verify with.
	teamIDv, _ := strconv.ParseInt(payload.Data.Object.Metadata.TeamID, 10, 64)
	secret := strings.TrimSpace(os.Getenv("PARATRACK_STRIPE_WEBHOOK_SECRET"))
	if teamIDv > 0 {
		if _, ws, err := s.db.TeamStripe(r.Context(), teamIDv); err == nil && ws != "" {
			secret = ws
		}
	}
	if !verifyStripeSignature(r.Header.Get("Stripe-Signature"), body, secret, time.Now()) {
		http.Error(w, "bad signature", 400)
		return
	}
	// Stripe's fulfillment rule: anything but "unpaid" (paid, or
	// no_payment_required for a zero total). Delayed methods complete as
	// unpaid and settle later with async_payment_succeeded.
	// https://docs.stripe.com/checkout/fulfillment
	paid := (payload.Type == "checkout.session.completed" && payload.Data.Object.PaymentStatus != "unpaid" &&
		payload.Data.Object.PaymentStatus != "") ||
		payload.Type == "checkout.session.async_payment_succeeded"
	invID, _ := strconv.ParseInt(payload.Data.Object.Metadata.InvoiceID, 10, 64)
	if !paid || invID == 0 || teamIDv == 0 {
		w.WriteHeader(200)
		return
	}
	if err := s.db.MarkInvoicePaid(r.Context(), teamIDv, invID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			w.WriteHeader(200) // not ours to retry
			return
		}
		http.Error(w, err.Error(), 500) // Stripe retries
		return
	}
	s.audit(r, "invoice.paid", strconv.FormatInt(invID, 10), "stripe")
	s.fireWebhookTeam(teamIDv, "invoice.paid", map[string]any{"invoice_id": invID, "team_id": teamIDv})
	w.WriteHeader(200)
}

// verifyStripeSignature checks a Stripe-Signature header
// ("t=<unix>,v1=<hex>[,v1=…]"): HMAC-SHA256 of "t.body" with the whsec_
// secret, within 5 minutes. No secret means nothing can be verified.
func verifyStripeSignature(header string, body []byte, secret string, now time.Time) bool {
	if secret == "" || header == "" {
		return false
	}
	var ts string
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	t, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || now.Sub(time.Unix(t, 0)).Abs() > 5*time.Minute {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	want := mac.Sum(nil)
	for _, sig := range sigs {
		if got, err := hex.DecodeString(sig); err == nil && hmac.Equal(got, want) {
			return true
		}
	}
	return false
}

// handleInvoiceMarkPaid is the manual "record payment" button.
func (s *Server) handleInvoiceMarkPaid(w http.ResponseWriter, r *http.Request) {
	inv, _, _, ok := s.loadInvoiceVM(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.db.MarkInvoicePaid(r.Context(), teamID(r), inv.ID); err != nil {
		http.Redirect(w, r, "/invoices/"+strconv.FormatInt(inv.ID, 10)+"?flash="+encodeFlash(false, err.Error()),
			http.StatusSeeOther)
		return
	}
	s.audit(r, "invoice.paid", inv.Number, "manual")
	http.Redirect(w, r, "/invoices/"+strconv.FormatInt(inv.ID, 10)+"?flash="+encodeFlash(true, "marked paid"),
		http.StatusSeeOther)
}

// handleTeamStripe saves Stripe credentials (team settings form).
func (s *Server) handleTeamStripe(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	key := strings.TrimSpace(r.PostForm.Get("stripe_key"))
	secret := strings.TrimSpace(r.PostForm.Get("stripe_webhook_secret"))
	if err := s.db.SetTeamStripe(r.Context(), teamID(r), key, secret); err != nil {
		http.Redirect(w, r, "/settings/team?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	s.audit(r, "team.stripe_update", "", "")
	http.Redirect(w, r, "/settings/team?flash="+encodeFlash(true, "updated"), http.StatusSeeOther)
}

// loadInvoiceVM fetches the invoice and its lines as a view-model.
// The URL is /invoices/{id}[/pdf|/pay|/paid] — id is the first segment.
func (s *Server) loadInvoiceVM(r *http.Request) (db.Invoice, []db.InvoiceLine, invoiceVM, bool) {
	path := strings.TrimPrefix(r.URL.Path, "/invoices/")
	idStr := strings.SplitN(path, "/", 2)[0]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return db.Invoice{}, nil, invoiceVM{}, false
	}
	inv, err := s.db.GetInvoice(r.Context(), teamID(r), id)
	if err != nil {
		return db.Invoice{}, nil, invoiceVM{}, false
	}
	lines, _ := s.db.ListInvoiceLines(r.Context(), inv.ID)
	total, secs := 0, 0
	vms := make([]invoiceLineVM, 0, len(lines))
	for _, l := range lines {
		total += l.AmountCents
		secs += db.HoursHundredths(l.Seconds)
		vms = append(vms, invoiceLineVM{
			Label: l.Label, Hours: fmtHoursL(resolveLang(r), db.HoursHundredths(l.Seconds)),
			Rate: moneyL(resolveLang(r), l.RateCents, inv.Currency), Amount: moneyL(resolveLang(r), l.AmountCents, inv.Currency),
			RateCents: l.RateCents, AmountCents: l.AmountCents, Seconds: l.Seconds,
		})
	}
	vm := invoiceVM{
		ID: inv.ID, Number: inv.Number, ClientName: inv.ClientName,
		PeriodLabel: fmtDate(resolveLang(r), inv.PeriodStart) + " – " + fmtDate(resolveLang(r), inv.PeriodEnd.AddDate(0, 0, -1)),
		PeriodISO:   inv.PeriodStart.Format("2006-01-02") + " – " + inv.PeriodEnd.AddDate(0, 0, -1).Format("2006-01-02"),
		Status:      inv.Status, Notes: inv.Notes, Lines: vms,
		Total: moneyL(resolveLang(r), total, inv.Currency), TotalCents: total, Hours: fmtHoursL(resolveLang(r), secs),
		PaymentURL: inv.PaymentURL, Currency: inv.Currency, TeamID: inv.TeamID,
		IssuedLabel:   fmtDate(resolveLang(r), inv.CreatedAt.Local()),
		SellerDetails: inv.SellerDetails, ClientDetails: inv.ClientDetails, VATNote: inv.VATNote,
	}
	return inv, lines, vm, true
}

// createStripeCheckout opens a Stripe Checkout session and returns the
// hosted URL + session id. Uses the form-encoded legacy endpoint so we
// need no SDK.
func createStripeCheckout(secret string, vm invoiceVM, successURL string) (string, string, error) {
	if vm.TotalCents <= 0 {
		return "", "", fmt.Errorf("invoice total must be positive")
	}
	form := url.Values{}
	form.Set("mode", "payment")
	form.Set("success_url", successURL+"?flash=paid")
	form.Set("cancel_url", successURL)
	form.Set("client_reference_id", strconv.FormatInt(vm.ID, 10))
	form.Set("metadata[invoice_id]", strconv.FormatInt(vm.ID, 10))
	form.Set("metadata[team_id]", strconv.FormatInt(vm.TeamID, 10))
	form.Set("line_items[0][quantity]", "1")
	// The invoice's own currency: unit_amount is in its minor units.
	form.Set("line_items[0][price_data][currency]", strings.ToLower(vm.Currency))
	form.Set("line_items[0][price_data][unit_amount]", strconv.Itoa(vm.TotalCents))
	label := "Invoice " + vm.Number
	if len(label) > 100 {
		label = label[:100]
	}
	form.Set("line_items[0][price_data][product_data][name]", label)
	req, err := http.NewRequest("POST", "https://api.stripe.com/v1/checkout/sessions",
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("stripe %d: %s", resp.StatusCode, string(b)[:min(180, len(b))])
	}
	var out struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", "", err
	}
	if out.URL == "" {
		return "", "", fmt.Errorf("stripe returned no url")
	}
	return out.URL, out.ID, nil
}

func stripeKeyFromEnv() string {
	return strings.TrimSpace(os.Getenv("PARATRACK_STRIPE_KEY"))
}
