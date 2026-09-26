package web

import (
	"net/http"
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
	if v := strings.TrimSpace(r.PostForm.Get("rate_cents")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			http.Redirect(w, r, "/projects/"+slug+"?flash="+encodeFlash(false, "rate must be a number of cents"),
				http.StatusSeeOther)
			return
		}
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
	Label     string
	Hours     string
	Rate      string // "45.00"
	Amount    string // "180.00"
	RateCents int
	AmountCents int
	Seconds   int
}

type invoiceVM struct {
	ID           int64
	Number       string
	ClientName   string
	PeriodLabel  string
	Status       string
	Notes        string
	Lines        []invoiceLineVM
	Total        string
	TotalCents   int
	Hours        string
	PaymentURL   string
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
			secs += l.Seconds
		}
		data.Items = append(data.Items, invoiceSummary{
			ID:       inv.ID,
			Number:   inv.Number,
			Client:   inv.ClientName,
			Status:   inv.Status,
			Total:    formatMoney(total),
			Hours:    fmtDuration(secs),
			Period:   inv.PeriodStart.Format("Jan 2") + " – " + inv.PeriodEnd.Format("Jan 2"),
		})
	}
	// default window: this month
	now := time.Now()
	data.DefStart = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
	data.DefEnd = now.Format("2006-01-02")
	if projects, err := s.db.ListProjects(r.Context(), teamID(r), false); err == nil {
		data.Projects = projects
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
	if err != nil {
		http.Redirect(w, r, "/invoices?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	_ = now
	s.audit(r, "invoice.create", inv.Number, inv.ClientName)
	s.fireWebhook(r, "invoice.created", map[string]any{
		"invoice_id": inv.ID, "number": inv.Number, "client": inv.ClientName,
	})
	http.Redirect(w, r, "/invoices/"+strconv.FormatInt(inv.ID, 10), http.StatusSeeOther)
}

// handleInvoiceDetail renders one invoice (print-ready).
func (s *Server) handleInvoiceDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/invoices/"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	inv, err := s.db.GetInvoice(r.Context(), teamID(r), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	lines, _ := s.db.ListInvoiceLines(r.Context(), inv.ID)
	lang := string(resolveLang(r))
	total := 0
	secs := 0
	vms := make([]invoiceLineVM, 0, len(lines))
	for _, l := range lines {
		total += l.AmountCents
		secs += l.Seconds
		vms = append(vms, invoiceLineVM{
			Label:       l.Label,
			Hours:       fmtDuration(l.Seconds),
			Rate:        formatMoney(l.RateCents),
			Amount:      formatMoney(l.AmountCents),
			RateCents:   l.RateCents,
			AmountCents: l.AmountCents,
			Seconds:     l.Seconds,
		})
	}
	data := invoiceDetailPage{
		pageData: pageData{Title: inv.Number, Active: "invoices", Lang: lang},
		Inv: invoiceVM{
			ID:          inv.ID,
			Number:      inv.Number,
			ClientName:  inv.ClientName,
			PeriodLabel: inv.PeriodStart.Format("Jan 2, 2006") + " – " + inv.PeriodEnd.Format("Jan 2, 2006"),
			Status:      inv.Status,
			Notes:       inv.Notes,
			Lines:       vms,
			Total:       formatMoney(total),
			TotalCents:  total,
			Hours:       fmtDuration(secs),
			PaymentURL:  inv.PaymentURL,
		},
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, inv.Number, "invoices", "invoice-detail", &data)
}

// invoiceDetailPage is /invoices/{id}.
type invoiceDetailPage struct {
	pageData
	Inv     invoiceVM
	Flash   string
	FlashOK bool
}

func (p *invoiceDetailPage) setCSRF(t string) { p.pageData.setCSRF(t) }

// handleInvoiceStatus flips draft → sent → paid.
func (s *Server) handleInvoiceStatus(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/invoices/"), 10, 64)
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
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/invoices/"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_ = s.db.DeleteInvoice(r.Context(), teamID(r), id)
	http.Redirect(w, r, "/invoices?flash=removed", http.StatusSeeOther)
}

// formatMoney renders cents as "12.34".
func formatMoney(cents int) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return sign + strconv.Itoa(cents/100) + "." + twoDigits(cents%100)
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
