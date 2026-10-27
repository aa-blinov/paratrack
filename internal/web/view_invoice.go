package web

import (
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
)

type invoiceLineVM struct {
	Label       string
	Hours       string
	Rate        string
	Amount      string
	RateCents   int
	AmountCents int
	Seconds     int
}

type invoiceVM struct {
	ID          int64
	Number      string
	ClientName  string
	PeriodLabel string
	PeriodISO   string
	Status      string
	Notes       string
	Lines       []invoiceLineVM
	Total       string
	TotalCents  int
	// VATIncluded names the VAT inside the total, when the invoice carries a rate (FIN-16).
	VATIncluded string
	Hours       string
	// DriftMessage tells a draft that its tracked time moved away from its lines.
	DriftMessage                          string
	PaymentURL                            string
	Currency                              string
	IssuedLabel                           string
	SellerDetails, ClientDetails, VATNote string
	ClientEmail, Receipt                  string
	DueISO, DueLabel                      string
	Overdue                               bool
	TeamID                                int64
	Logo                                  template.URL
	// The payment journal (FIN-08): what is paid, what is still owed, and the entries.
	PaidLabel, RemainingLabel string
	Remaining, PartialPaid    bool
	TodayISO                  string
	Payments                  []paymentVM
}

// paymentVM is one entry of the payment journal on the invoice page.
type paymentVM struct {
	Date, Amount, Note, Source string
}

type invoiceProjectOpt struct {
	ID                                     int64
	Name                                   string
	ClientName, ClientDetails, ClientEmail string
	Selected, Eligible                     bool
}

type unassignedActivityView struct {
	ID       int64
	Sessions int64
	Name     string
	Billed   bool
}

type unbilledView struct {
	ProjectID         int64
	ProjectName, Slug string
	Hours, Amount     string
	Since, SinceISO   string
}

type invoiceSummary struct {
	ID      int64
	Number  string
	Client  string
	Status  string
	Total   string
	Hours   string
	Period  string
	Due     string
	Overdue bool
	Issued  string
	Partial string
}

type invoicesPage struct {
	pageData
	InvoicesReact bool
	// Reconcile is the time worked against the invoices, for a window the manager chose (FIN-04).
	Reconcile  *reconcileView
	Items      []invoiceSummary
	Projects   []invoiceProjectOpt
	Prefill    invoiceProjectOpt
	Unbilled   []unbilledView
	Unassigned []unassignedActivityView
	Billable   bool
	DefStart   string
	DefEnd     string
	// Money still owed and money already collected, each grouped by currency.
	// A client pays a number; this is the number to compare it against.
	Outstanding    string
	Paid           string
	HasOutstanding bool
	// RequisitesEmpty is set while the issuer's details are blank: every invoice then
	// carries only the name, so the screen says so before one is issued (ADM-17).
	RequisitesEmpty bool
	// Money is priced from rounded time, so the screen has to say so: a person
	// comparing "1 ч 28 мин" with the amount has to know why they differ.
	RoundMinutes int
	RoundMode    string
	Flash        string
	FlashOK      bool
}

func (p *invoicesPage) setCSRF(t string)   { p.pageData.setCSRF(t) }
func (p *invoicesPage) usesReactApp() bool { return p.InvoicesReact }

type invoiceDetailPage struct {
	pageData
	InvoiceReact    bool
	InvoiceDetail   bool
	InvoiceActReact bool
	Inv             invoiceVM
	Seller          string
	StripeReady     bool
	MailReady       bool
	MailtoURL       string
	Flash           string
	FlashOK         bool
}

func (p *invoiceDetailPage) setCSRF(t string)   { p.pageData.setCSRF(t) }
func (p *invoiceDetailPage) usesReactApp() bool { return p.InvoiceReact || p.InvoiceActReact }

// loadInvoiceVM builds the shared invoice presentation used by the page and documents.
func (s *Server) loadInvoiceVM(r *http.Request, includeStripeReadiness bool) (model.Invoice, invoiceVM, bool, error) {
	path := strings.TrimPrefix(r.URL.Path, "/invoices/")
	idStr := strings.SplitN(path, "/", 2)[0]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		return model.Invoice{}, invoiceVM{}, false, model.ErrNotFound
	}
	return s.invoiceVMFor(r, id, includeStripeReadiness)
}

// invoiceVMFor builds the shared invoice presentation of one invoice in the request's
// workspace. A client's link reaches it without the invoice path in the address.
func (s *Server) invoiceVMFor(r *http.Request, id int64, includeStripeReadiness bool) (model.Invoice, invoiceVM, bool, error) {
	snapshot, err := s.services.InvoiceDocuments.Build(r.Context(), appmodel.InvoiceDocumentRequest{
		TeamID: teamID(r), InvoiceID: id, IncludeStripeReadiness: includeStripeReadiness,
	})
	if err != nil {
		return model.Invoice{}, invoiceVM{}, false, err
	}
	details := snapshot.Details
	inv, lines := details.Invoice, details.Lines
	names, err := s.executorNamesToHide(r, inv)
	if err != nil {
		return model.Invoice{}, invoiceVM{}, false, err
	}
	vms := make([]invoiceLineVM, 0, len(lines))
	lang := resolveLang(r)
	for _, line := range lines {
		vms = append(vms, invoiceLineVM{
			Label: withoutExecutor(line.Label, names), Hours: fmtHoursL(lang, money.HoursHundredths(line.Seconds)),
			Rate: moneyL(lang, line.RateCents, inv.Currency), Amount: moneyL(lang, line.AmountCents, inv.Currency),
			RateCents: line.RateCents, AmountCents: line.AmountCents, Seconds: line.Seconds,
		})
	}
	vm := invoiceVM{
		ID: inv.ID, Number: inv.Number, ClientName: inv.ClientName,
		PeriodLabel: periodSpan(lang, fmtDate(lang, inv.PeriodStart), fmtDate(lang, inv.PeriodEnd.AddDate(0, 0, -1))),
		PeriodISO:   inv.PeriodStart.Format("2006-01-02") + " – " + inv.PeriodEnd.AddDate(0, 0, -1).Format("2006-01-02"),
		Status:      inv.Status, Notes: inv.Notes, Lines: vms,
		Total: moneyL(lang, details.TotalCents, inv.Currency), TotalCents: details.TotalCents,
		Hours:      fmtHoursL(lang, details.TotalHoursHundredths),
		PaymentURL: inv.PaymentURL, Currency: inv.Currency, TeamID: inv.TeamID,
		IssuedLabel:   fmtDate(lang, inv.CreatedAt.In(userLoc(r))),
		SellerDetails: inv.SellerDetails, ClientDetails: inv.ClientDetails, VATNote: inv.VATNote,
		ClientEmail: inv.ClientEmail, Receipt: inv.Receipt,
		DueISO: inv.DueDate, DueLabel: invoiceDueLabel(lang, inv.DueDate),
		Overdue: invoiceOverdue(inv.Status, inv.DueDate, userNow(r).Format("2006-01-02")),
	}
	vm.DriftMessage = invoiceDriftMessage(lang, inv.Status, details.InvoicedSeconds, details.TrackedSeconds)
	paid, owed := journalTotals(inv.Status, details.TotalCents, details.PaidCents)
	vm.PaidLabel = moneyL(lang, paid, inv.Currency)
	vm.RemainingLabel = moneyL(lang, owed, inv.Currency)
	vm.Remaining = inv.Status == "sent" && owed > 0
	vm.PartialPaid = details.PaidCents > 0 && owed > 0
	vm.TodayISO = userNow(r).Format("2006-01-02")
	vm.Payments = make([]paymentVM, 0, len(details.Payments))
	for _, p := range details.Payments {
		vm.Payments = append(vm.Payments, paymentVM{
			Date: paymentDay(lang, p.PaidOn), Amount: moneyL(lang, p.AmountCents, inv.Currency),
			Note: p.Note, Source: paymentSource(lang, p.Source),
		})
	}
	vm.Logo = logoURL(snapshot.BillingRules.Logo)
	if inv.VATRate > 0 {
		vm.VATIncluded = fmt.Sprintf(i18n.T(lang, "inv.vatIncluded"), inv.VATRate, moneyL(lang, vatIncludedCents(details.TotalCents, inv.VATRate), inv.Currency))
	}
	return inv, vm, snapshot.StripeReady, nil
}

// invoiceOverdue marks a sent invoice that is still unpaid past its due day
// (FIN-07). A draft is not overdue: nothing has been asked of the client yet.
func invoiceOverdue(status, due, today string) bool {
	return due != "" && due < today && status != "draft" && status != "paid"
}

// invoiceDueLabel prints the due day the way the document prints its dates,
// or nothing when the invoice has no due day.
func invoiceDueLabel(lang i18n.Lang, due string) string {
	day, err := time.Parse("2006-01-02", due)
	if due == "" || err != nil {
		return ""
	}
	return fmtDay(lang, day)
}

// invoiceDriftMessage names the two hours of a draft whose tracked time moved away
// from its lines. A sent or paid invoice never drifts: its sessions are locked. An
// empty message means no drift.
func invoiceDriftMessage(lang i18n.Lang, status string, invoicedSeconds, trackedSeconds int) string {
	if status != "draft" || invoicedSeconds == trackedSeconds {
		return ""
	}
	return fmt.Sprintf(i18n.T(lang, "inv.drift"),
		fmtHoursL(lang, money.HoursHundredths(invoicedSeconds)),
		fmtHoursL(lang, money.HoursHundredths(trackedSeconds)))
}

func unbilledViewsFrom(list []appmodel.UnbilledProject, r *http.Request) []unbilledView {
	lang := resolveLang(r)
	out := make([]unbilledView, 0, len(list))
	for _, item := range list {
		if item.Hundredths == 0 {
			continue
		}
		localSince := item.Since.In(userLoc(r))
		out = append(out, unbilledView{
			ProjectID: item.ProjectID, ProjectName: item.ProjectName, Slug: item.ProjectSlug,
			Hours: fmtHoursL(lang, item.Hundredths), Amount: moneyL(lang, item.AmountCents, item.Currency),
			Since: fmtDay(lang, localSince), SinceISO: localSince.Format("2006-01-02"),
		})
	}
	return out
}

func (s *Server) sellerName(r *http.Request) string {
	t, ok := TeamFrom(r.Context())
	if !ok {
		return "paratrack"
	}
	if strings.HasPrefix(t.Slug, fmt.Sprintf("personal-%d-", t.OwnerID)) {
		user, err := s.services.Auth.Identity.IdentityByID(r.Context(), t.OwnerID)
		if err != nil {
			if s.logger != nil {
				s.logger.Printf("web: load invoice issuer name for user %d: %v", t.OwnerID, err)
			}
		} else if strings.TrimSpace(user.Name) != "" {
			return user.Name
		}
	}
	return t.Name
}

// paymentDay prints the day the money arrived the way the document prints dates.
func paymentDay(lang i18n.Lang, iso string) string {
	day, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	return fmtDay(lang, day)
}

// paymentSource names how a journal entry came in.
func paymentSource(lang i18n.Lang, source string) string {
	if source == "stripe" {
		return i18n.T(lang, "inv.paySourceStripe")
	}
	return i18n.T(lang, "inv.paySourceManual")
}

// journalTotals is what an invoice has been paid and what it still owes. An
// invoice marked paid before the journal existed has no entries, and is paid in full.
func journalTotals(status string, total, journal int) (paid, owed int) {
	if status == "paid" {
		return total, 0
	}
	return journal, total - journal
}

// executorNamesToHide lists the names a by-person invoice must leave off its lines while the
// workspace hides them: the members' and former members' names and emails, the way the
// line label spells a person (FIN-15). Nil when nothing is hidden.
func (s *Server) executorNamesToHide(r *http.Request, inv model.Invoice) (map[string]bool, error) {
	if !inv.ByPerson {
		return nil, nil
	}
	settings, err := s.services.Teams.Settings.Settings(r.Context(), teamID(r))
	if err != nil {
		return nil, fmt.Errorf("load invoice settings: %w", err)
	}
	if !settings.HideExecutorNames {
		return nil, nil
	}
	snapshot, err := s.services.MemberAdmin.Management(r.Context(), teamID(r))
	if err != nil {
		return nil, fmt.Errorf("load members for the invoice: %w", err)
	}
	names := map[string]bool{}
	for _, member := range snapshot.Members {
		names[member.Name], names[member.Email] = true, true
	}
	for _, former := range snapshot.Former {
		names[former.Name], names[former.Email] = true, true
	}
	return names, nil
}

// withoutExecutor drops the member's name from a by-person line label. The name is the last
// segment of the label, so only a last segment that names a member is taken off (FIN-15).
func withoutExecutor(label string, names map[string]bool) string {
	i := strings.LastIndex(label, " · ")
	if i < 0 || !names[label[i+len(" · "):]] {
		return label
	}
	return label[:i]
}

// vatIncludedCents is the VAT inside a price that already includes it: total × rate ÷ (100 +
// rate), rounded half up to the cent (FIN-16).
func vatIncludedCents(totalCents, ratePercent int) int {
	if ratePercent <= 0 {
		return 0
	}
	denominator := 100 + ratePercent
	return (totalCents*ratePercent*2 + denominator) / (2 * denominator)
}
