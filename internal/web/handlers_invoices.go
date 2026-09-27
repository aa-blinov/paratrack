package web

import (
	"errors"
	"fmt"
	dbpkg "github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

// ---------------------------------------------------------------------------
// Wave 3: billable rates + invoices
// ---------------------------------------------------------------------------

// handleProjectRate saves the billable rate for a project.
// Form: slug, rate_cents, billable (1/0).
func (s *Server) handleProjectRate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	slug := strings.TrimSpace(r.PostForm.Get("slug"))
	p, err := s.db.GetProjectBySlug(r.Context(), teamID(r), slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var rate *int
	if n, has, err := formCents(r, "rate"); err != nil {
		http.Redirect(w, r, "/projects/"+slug+"?flash="+encodeFlash(false, i18n.T(resolveLang(r), "bill.badRate")),
			http.StatusSeeOther)
		return
	} else if has {
		rate = &n
	}
	var billable *bool
	if v := strings.TrimSpace(r.PostForm.Get("billable")); v != "" {
		b := v == "1" || v == "true" || v == "on"
		billable = &b
	}
	if err := s.db.SetProjectRate(r.Context(), teamID(r), p.ID, rate, billable); err != nil {
		http.Redirect(w, r, "/projects/"+slug+"?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/projects/"+slug+"?flash="+encodeFlash(true, "updated"), http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Invoices
// ---------------------------------------------------------------------------

type invoiceLineVM struct {
	Label       string
	Hours       string
	Rate        string // "45.00"
	Amount      string // "180.00"
	RateCents   int
	AmountCents int
	Seconds     int
}

type invoiceVM struct {
	ID          int64
	Number      string
	ClientName  string
	PeriodLabel string
	PeriodISO   string // PDF: the core font has no Cyrillic month names
	Status      string
	Notes       string
	Lines       []invoiceLineVM
	Total       string
	TotalCents  int
	Hours       string
	PaymentURL  string
	Currency    string
	IssuedLabel string // creation date, shown as the invoice date
	SellerDetails, ClientDetails, VATNote string
	TeamID      int64
}

// handleInvoices lists invoices and offers a generator form.
func (s *Server) handleInvoices(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.ListInvoices(r.Context(), teamID(r))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	lang := string(resolveLang(r))
	data := invoicesPage{
		pageData: pageData{Title: "Invoices", Active: "invoices", Lang: lang},
	}
	for _, inv := range list {
		lines, _ := s.db.ListInvoiceLines(r.Context(), inv.ID)
		total := 0
		secs := 0
		for _, l := range lines {
			total += l.AmountCents
			secs += dbpkg.HoursHundredths(l.Seconds)
		}
		data.Items = append(data.Items, invoiceSummary{
			ID:     inv.ID,
			Number: inv.Number,
			Client: inv.ClientName,
			Status: inv.Status,
			Total:  moneyL(resolveLang(r), total, inv.Currency),
			Hours:  fmtHoursL(resolveLang(r), secs),
			Period: fmtDay(resolveLang(r), inv.PeriodStart) + " – " + fmtDay(resolveLang(r), inv.PeriodEnd.AddDate(0, 0, -1)),
		})
	}
	// default window: this month
	now := time.Now()
	data.DefStart = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
	data.DefEnd = now.Format("2006-01-02")
	if projects, err := s.db.ListProjects(r.Context(), teamID(r), false); err == nil {
		data.Projects = projects
		for _, p := range projects {
			if p.Billable && p.BillableRateCents != nil && *p.BillableRateCents > 0 {
				data.Billable = true
			}
		}
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, "Invoices", "invoices", "invoices", &data)
}

type invoiceSummary struct {
	ID     int64
	Number string
	Client string
	Status string
	Total  string
	Hours  string
	Period string
}

// invoicesPage is the /invoices envelope.
type invoicesPage struct {
	pageData
	Items    []invoiceSummary
	Projects []model.Project
	Billable bool // any project with a rate; otherwise invoices come out empty
	DefStart string
	DefEnd   string
	Flash    string
	FlashOK  bool
}

func (p *invoicesPage) setCSRF(t string) { p.pageData.setCSRF(t) }

// handleInvoiceCreate generates a draft from tracked time.
func (s *Server) handleInvoiceCreate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	client := strings.TrimSpace(r.PostForm.Get("client"))
	startStr := strings.TrimSpace(r.PostForm.Get("start"))
	endStr := strings.TrimSpace(r.PostForm.Get("end"))
	notes := strings.TrimSpace(r.PostForm.Get("notes"))
	projectStr := strings.TrimSpace(r.PostForm.Get("project_id"))
	now := time.Now()
	start, err1 := time.ParseInLocation("2006-01-02", startStr, time.Local)
	end, err2 := time.ParseInLocation("2006-01-02", endStr, time.Local)
	if err1 != nil || err2 != nil || end.Before(start) {
		http.Redirect(w, r, "/invoices?flash="+encodeFlash(false, "bad period"), http.StatusSeeOther)
		return
	}
	// end is exclusive: make it the end of the chosen day
	end = end.AddDate(0, 0, 1)
	var projectID int64
	if projectStr != "" {
		projectID, _ = strconv.ParseInt(projectStr, 10, 64)
	}
	lines, err := s.db.BuildInvoiceLines(r.Context(), teamID(r), start, end, projectID)
	if err != nil {
		http.Redirect(w, r, "/invoices?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	if len(lines) == 0 {
		http.Redirect(w, r, "/invoices?flash="+encodeFlash(false, "no billable time in that period"), http.StatusSeeOther)
		return
	}
	number, err := s.db.NextInvoiceNumber(r.Context(), teamID(r))
	if err != nil {
		http.Redirect(w, r, "/invoices?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	inv, err := s.db.CreateInvoice(r.Context(), teamID(r), number, client, start, end, notes, lines)
	if errors.Is(err, dbpkg.ErrMixedCurrency) {
		http.Redirect(w, r, "/invoices?flash="+encodeFlash(false, "mixed currency"), http.StatusSeeOther)
		return
	}
	if err != nil {
		http.Redirect(w, r, "/invoices?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	_ = now
	seller, vat, _ := s.db.TeamRequisites(r.Context(), teamID(r))
	_ = s.db.SetInvoiceParties(r.Context(), teamID(r), inv.ID, seller,
		strings.TrimSpace(r.PostForm.Get("client_details")), vat)
	s.audit(r, "invoice.create", inv.Number, inv.ClientName)
	s.fireWebhook(r, "invoice.created", map[string]any{
		"invoice_id": inv.ID, "number": inv.Number, "client": inv.ClientName,
	})
	// Same hours on two documents? Say so on the new one; the user decides.
	labels := make([]string, 0, len(lines))
	for _, l := range lines {
		labels = append(labels, l.Label)
	}
	if nums, _ := s.db.OverlappingInvoices(r.Context(), teamID(r), inv.ID, start, end, labels); len(nums) > 0 {
		msg := fmt.Sprintf(i18n.T(resolveLang(r), "inv.overlap"), strings.Join(nums, ", "))
		http.Redirect(w, r, "/invoices/"+strconv.FormatInt(inv.ID, 10)+"?flash="+url.QueryEscape(encodeFlash(false, msg)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/invoices/"+strconv.FormatInt(inv.ID, 10), http.StatusSeeOther)
}

// handleInvoiceDetail renders one invoice (print-ready). Same view model
// as the PDF (loadInvoiceVM), so screen and file never disagree.
func (s *Server) handleInvoiceDetail(w http.ResponseWriter, r *http.Request) {
	inv, _, vm, ok := s.loadInvoiceVM(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	data := invoiceDetailPage{
		pageData: pageData{Title: inv.Number, Active: "invoices", Lang: string(resolveLang(r))},
		Inv:      vm,
	}
	if t, ok := TeamFrom(r.Context()); ok {
		data.Seller = t.Name
	}
	if key, _, err := s.db.TeamStripe(r.Context(), teamID(r)); (err == nil && key != "") || stripeKeyFromEnv() != "" {
		data.StripeReady = true
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, inv.Number, "invoices", "invoice-detail", &data)
}

// invoiceDetailPage is /invoices/{id}.
type invoiceDetailPage struct {
	pageData
	Inv         invoiceVM
	Seller      string // workspace name, shown as the issuer
	StripeReady bool   // online payment link only when Stripe is set up
	Flash       string
	FlashOK     bool
}

func (p *invoiceDetailPage) setCSRF(t string) { p.pageData.setCSRF(t) }

// handleInvoiceStatus flips draft → sent → paid.
func (s *Server) handleInvoiceStatus(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	status := strings.TrimSpace(r.PostForm.Get("status"))
	if err := s.db.UpdateInvoiceStatus(r.Context(), teamID(r), id, status); err != nil {
		http.Redirect(w, r, "/invoices/"+strconv.FormatInt(id, 10)+"?flash="+encodeFlash(false, err.Error()),
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
	_ = s.db.DeleteInvoice(r.Context(), teamID(r), id)
	http.Redirect(w, r, "/invoices?flash=removed", http.StatusSeeOther)
}

// formatMoney renders cents as "12.34".
// formCents reads a money amount: "<name>" in currency units ("45.50",
// "45,5") from the UI, or "<name>_cents" as an integer from API clients.
func formCents(r *http.Request, name string) (cents int, has bool, err error) {
	bad := errors.New("bad amount")
	if v := strings.TrimSpace(r.Form.Get(name)); v != "" {
		// "2 500,50", "2500,50" and "2500.50" all mean the same amount.
		v = strings.NewReplacer(" ", "", "\u00a0", "", "\u202f", "", ",", ".").Replace(v)
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < 0 {
			return 0, true, bad
		}
		return int(math.Round(f * 100)), true, nil
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
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return sign + strconv.Itoa(cents/100) + "." + twoDigits(cents%100)
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

func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
