package web

import (
	"html/template"
	netmail "net/mail"

	"errors"
	"fmt"
	dbpkg "github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/mail"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
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
	ID                                    int64
	Number                                string
	ClientName                            string
	PeriodLabel                           string
	PeriodISO                             string // PDF: the core font has no Cyrillic month names
	Status                                string
	Notes                                 string
	Lines                                 []invoiceLineVM
	Total                                 string
	TotalCents                            int
	Hours                                 string
	PaymentURL                            string
	Currency                              string
	IssuedLabel                           string // creation date, shown as the invoice date
	SellerDetails, ClientDetails, VATNote string
	ClientEmail, Receipt                  string
	TeamID                                int64
	Logo                                  template.URL // the workspace logo, now (not frozen at issue)
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
	now := userNow(r)
	data.DefStart = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
	data.DefEnd = now.Format("2006-01-02")
	want, _ := strconv.ParseInt(r.URL.Query().Get("project"), 10, 64)
	if from := r.URL.Query().Get("from"); from != "" {
		if _, err := time.Parse("2006-01-02", from); err == nil {
			data.DefStart = from
		}
	}
	if projects, err := s.db.ListProjects(r.Context(), teamID(r), false); err == nil {
		for _, p := range projects {
			if p.Billable && p.BillableRateCents != nil && *p.BillableRateCents > 0 {
				data.Billable = true
			}
			opt := invoiceProjectOpt{ID: p.ID, Name: p.Name, Selected: p.ID == want}
			if c, err := s.db.GetProjectClient(r.Context(), teamID(r), p.ID); err == nil {
				opt.ClientName, opt.ClientDetails, opt.ClientEmail = c.Name, c.Details, c.Email
			}
			if opt.Selected {
				data.Prefill = opt
			}
			data.Projects = append(data.Projects, opt)
		}
	}
	data.Unbilled = s.unbilledViews(r, 0)
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, "Invoices", "invoices", "invoices", &data)
}

// invoiceProjectOpt is a project in the invoice form, with the client it
// last billed so picking the project fills the rest.
type invoiceProjectOpt struct {
	ID                                     int64
	Name                                   string
	ClientName, ClientDetails, ClientEmail string
	Selected                               bool
}

// unbilledView is one project's "not invoiced yet" line.
type unbilledView struct {
	ProjectID         int64
	ProjectName, Slug string
	Hours, Amount     string
	Since, SinceISO   string
}

// unbilledViews is billable time not on any invoice, per project
// (projectID > 0: just that one).
func (s *Server) unbilledViews(r *http.Request, projectID int64) []unbilledView {
	list, err := s.db.Unbilled(r.Context(), teamID(r), projectID)
	if err != nil {
		return nil
	}
	lang := resolveLang(r)
	out := make([]unbilledView, 0, len(list))
	for _, u := range list {
		if u.Hundredths == 0 {
			continue
		}
		out = append(out, unbilledView{
			ProjectID: u.ProjectID, ProjectName: u.ProjectName, Slug: u.ProjectSlug,
			Hours: fmtHoursL(lang, u.Hundredths), Amount: moneyL(lang, u.AmountCents, u.Currency),
			Since: fmtDay(lang, u.Since.In(userLoc(r))), SinceISO: u.Since.In(userLoc(r)).Format("2006-01-02"),
		})
	}
	return out
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
	Projects []invoiceProjectOpt
	Prefill  invoiceProjectOpt // the project picked via ?project=
	Unbilled []unbilledView
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
	now := userNow(r)
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
		projectID, _ = strconv.ParseInt(projectStr, 10, 64)
	}
	byPerson := r.PostForm.Get("by_person") == "1"
	lines, err := s.db.BuildInvoiceLinesFor(r.Context(), teamID(r), start, end, projectID, 0, byPerson)
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
	if errors.Is(err, dbpkg.ErrAlreadyBilled) {
		http.Redirect(w, r, "/invoices?flash=already_billed", http.StatusSeeOther)
		return
	}
	if err != nil {
		http.Redirect(w, r, "/invoices?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	_ = now
	seller, vat, _ := s.db.TeamRequisites(r.Context(), teamID(r))
	clientDetails := strings.TrimSpace(r.PostForm.Get("client_details"))
	clientEmail := strings.TrimSpace(r.PostForm.Get("client_email"))
	_ = s.db.SetInvoiceParties(r.Context(), teamID(r), inv.ID, seller, clientDetails, vat)
	_ = s.db.SetInvoiceProject(r.Context(), teamID(r), inv.ID, projectID, clientEmail)
	_ = s.db.SetInvoiceByPerson(r.Context(), teamID(r), inv.ID, byPerson)
	if projectID > 0 {
		// Next invoice for this project starts with the same client.
		_ = s.db.SetProjectClient(r.Context(), teamID(r), projectID,
			dbpkg.ProjectClient{Name: client, Details: clientDetails, Email: clientEmail})
	}
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
	data.Seller = s.sellerName(r)
	if key, _, err := s.db.TeamStripe(r.Context(), teamID(r)); (err == nil && key != "") || stripeKeyFromEnv() != "" {
		data.StripeReady = true
	}
	data.MailReady = mail.Configured()
	subj, body := invoiceMailText(resolveLang(r), vm, data.Seller)
	data.MailtoURL = "mailto:" + url.PathEscape(vm.ClientEmail) + "?subject=" + url.QueryEscape(subj) + "&body=" + url.QueryEscape(body)
	data.MailtoURL = strings.ReplaceAll(data.MailtoURL, "+", "%20")
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
	MailReady   bool   // SMTP configured: "send" mails the PDF itself
	MailtoURL   string // otherwise a draft in the user's mail app
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

// invoiceMailText is the letter as plain text (the mailto draft uses it).
func invoiceMailText(lang i18n.Lang, vm invoiceVM, seller string) (string, string) {
	subj, ev := invoiceEmail(lang, vm, seller)
	return subj, emailText(ev)
}

func (s *Server) invoiceBack(w http.ResponseWriter, r *http.Request, ok bool, msg string) {
	http.Redirect(w, r, "/invoices/"+r.PathValue("id")+"?flash="+url.QueryEscape(encodeFlash(ok, msg)), http.StatusSeeOther)
}

// handleInvoiceEdit changes who a draft is for and its notes.
func (s *Server) handleInvoiceEdit(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	inv, err := s.db.GetInvoice(r.Context(), teamID(r), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if inv.Status != "draft" {
		s.invoiceBack(w, r, false, i18n.T(resolveLang(r), "inv.onlyDraft"))
		return
	}
	f := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	if f("client") == "" {
		s.invoiceBack(w, r, false, i18n.T(resolveLang(r), "inv.clientRequired"))
		return
	}
	if err := s.db.UpdateInvoiceMeta(r.Context(), teamID(r), id, f("client"), f("client_details"), f("client_email"), f("notes")); err != nil {
		s.invoiceBack(w, r, false, err.Error())
		return
	}
	if inv.ProjectID > 0 {
		_ = s.db.SetProjectClient(r.Context(), teamID(r), inv.ProjectID,
			dbpkg.ProjectClient{Name: f("client"), Details: f("client_details"), Email: f("client_email")})
	}
	s.audit(r, "invoice.edit", inv.Number, "")
	s.invoiceBack(w, r, true, "updated")
}

// handleInvoiceRebuild recomputes a draft's lines from the ledger.
func (s *Server) handleInvoiceRebuild(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.db.RebuildInvoice(r.Context(), teamID(r), id); err != nil {
		s.invoiceBack(w, r, false, err.Error())
		return
	}
	s.audit(r, "invoice.rebuild", strconv.FormatInt(id, 10), "")
	s.invoiceBack(w, r, true, i18n.T(resolveLang(r), "inv.rebuilt"))
}

// handleInvoiceReceipt stores the "Мой налог" receipt of a payment.
func (s *Server) handleInvoiceReceipt(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	rc := strings.TrimSpace(r.FormValue("receipt"))
	if len(rc) > 300 {
		s.invoiceBack(w, r, false, "bad request")
		return
	}
	if err := s.db.SetInvoiceReceipt(r.Context(), teamID(r), id, rc); err != nil {
		s.invoiceBack(w, r, false, err.Error())
		return
	}
	s.invoiceBack(w, r, true, "updated")
}

// handleInvoiceSend mails the PDF to the client and marks a draft sent.
func (s *Server) handleInvoiceSend(w http.ResponseWriter, r *http.Request) {
	inv, _, vm, ok := s.loadInvoiceVM(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	lang := resolveLang(r)
	to := strings.TrimSpace(r.FormValue("to"))
	if _, err := netmail.ParseAddress(to); err != nil {
		s.invoiceBack(w, r, false, i18n.T(lang, "inv.badEmail"))
		return
	}
	if !mail.Configured() {
		s.invoiceBack(w, r, false, i18n.T(lang, "inv.mailOff"))
		return
	}
	seller := s.sellerName(r)
	pdf, err := renderInvoicePDF(vm, seller, lang)
	if err != nil {
		s.invoiceBack(w, r, false, err.Error())
		return
	}
	subj, ev := invoiceEmail(lang, vm, seller)
	msg, err := s.buildEmail(to, subj, ev, mail.Attachment{Name: invoicePDFName(vm), ContentType: "application/pdf", Data: pdf})
	if err == nil {
		err = s.deliver(msg)
	}
	if err != nil {
		s.invoiceBack(w, r, false, fmt.Sprintf(i18n.T(lang, "inv.mailFailed"), err.Error()))
		return
	}
	_ = s.db.SetInvoiceProject(r.Context(), teamID(r), inv.ID, inv.ProjectID, to)
	if inv.Status == "draft" {
		_ = s.db.UpdateInvoiceStatus(r.Context(), teamID(r), inv.ID, "sent")
	}
	s.audit(r, "invoice.send", inv.Number, to)
	s.invoiceBack(w, r, true, fmt.Sprintf(i18n.T(lang, "inv.mailed"), to))
}

// sellerName is who issues the documents: the workspace, or for a
// personal one ("Пространство: Аня") the owner's own name, which is what
// belongs on an invoice or act.
func (s *Server) sellerName(r *http.Request) string {
	t, ok := TeamFrom(r.Context())
	if !ok {
		return "paratrack"
	}
	if strings.HasPrefix(t.Slug, fmt.Sprintf("personal-%d-", t.OwnerID)) {
		if u, err := s.auth.FindByID(r.Context(), t.OwnerID); err == nil && strings.TrimSpace(u.Name) != "" {
			return u.Name
		}
	}
	return t.Name
}
