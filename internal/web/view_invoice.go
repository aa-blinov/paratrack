package web

import (
	"encoding/json"
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
	ID          int64
	Extra       bool
	Label       string
	Hours       string
	Rate        string
	Amount      string
	RateCents   int
	AmountCents int
	Seconds     int
}

type invoiceVM struct {
	// Seller is the issuer's name on the document: as issued once it was sent.
	Seller      string `json:"-"`
	VoidReason  string
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
	IssuedISO                             string `json:"-"`
	VATIncludedRU                         string `json:"-"`
	SellerDetails, ClientDetails, VATNote string
	ClientEmail, Receipt                  string
	// HasClientCard is a draft whose project has a client card the edit form
	// can update; without one the "update the card too" box is not offered.
	HasClientCard    bool
	DueISO, DueLabel string
	Overdue          bool
	TeamID           int64
	Logo             template.URL
	// The payment journal (FIN-08): what is paid, what is still owed, and the entries.
	PaidLabel, RemainingLabel string
	Remaining, PartialPaid    bool
	// RemainingCents is what is still owed: the payment QR asks for this,
	// not the total, once part of the invoice is paid.
	RemainingCents int
	TodayISO       string
	Payments       []paymentVM
}

// paymentVM is one entry of the payment journal on the invoice page.
type paymentVM struct {
	Date, Amount, Note, Source string
	ID                         int64
	// CanReverse is an ordinary entry no storno has reversed yet.
	CanReverse bool
}

type invoiceProjectOpt struct {
	ID                                     int64
	Name                                   string
	Rate                                   string // money per hour, empty when the project has none
	ClientName, ClientDetails, ClientEmail string
	Selected, Eligible                     bool
	// HasClientCard offers "update the client card too" on the new invoice.
	HasClientCard bool
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
	// Issued is the issue day, YYYY-MM-DD, empty for a draft: the register
	// filters on it and no other date stands in for it.
	Issued string
	// IssuedLabel is that day as the list prints it.
	IssuedLabel string
	Partial     string
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
		extra := line.Seconds == 0 && line.RateCents == 0
		hours := fmtHoursL(lang, money.HoursHundredths(line.Seconds))
		rate := moneyL(lang, line.RateCents, inv.Currency)
		if extra {
			hours = "—"
			rate = "—"
		}
		vms = append(vms, invoiceLineVM{
			ID: line.ID, Extra: extra,
			Label: withoutExecutor(line.Label, names), Hours: hours,
			Rate: rate, Amount: moneyL(lang, line.AmountCents, inv.Currency),
			RateCents: line.RateCents, AmountCents: line.AmountCents, Seconds: line.Seconds,
		})
	}
	loc := userLoc(r)
	vm := invoiceVM{
		ID: inv.ID, Number: inv.Number, ClientName: inv.ClientName,
		PeriodLabel: periodSpan(lang, fmtDate(lang, inv.PeriodStart.In(loc)), fmtDate(lang, inv.PeriodEnd.In(loc).AddDate(0, 0, -1))),
		PeriodISO:   inv.PeriodStart.In(loc).Format("2006-01-02") + " – " + inv.PeriodEnd.In(loc).AddDate(0, 0, -1).Format("2006-01-02"),
		Status:      inv.Status, Notes: inv.Notes, Lines: vms,
		Total: moneyL(lang, details.TotalCents, inv.Currency), TotalCents: details.TotalCents,
		Hours:      fmtHoursL(lang, details.TotalHoursHundredths),
		PaymentURL: inv.PaymentURL, Currency: inv.Currency, TeamID: inv.TeamID,
		IssuedLabel:   fmtDate(lang, inv.CreatedAt.In(userLoc(r))),
		IssuedISO:     inv.CreatedAt.In(userLoc(r)).Format("2006-01-02"),
		SellerDetails: inv.SellerDetails, ClientDetails: inv.ClientDetails, VATNote: inv.VATNote,
		ClientEmail: inv.ClientEmail, Receipt: inv.Receipt,
		DueISO: inv.DueDate, DueLabel: invoiceDueLabel(lang, inv.DueDate),
		Overdue: invoiceOverdue(inv.Status, inv.DueDate, userNow(r).Format("2006-01-02")),
	}
	vm.DriftMessage = invoiceDriftMessage(lang, inv.Status, details.InvoicedSeconds, details.TrackedSeconds)
	if inv.Status == "draft" && inv.ProjectID > 0 {
		if card, err := s.services.Clients.ForProject(r.Context(), appmodel.ProjectScopeQuery{TeamID: inv.TeamID, ProjectID: inv.ProjectID}); err == nil && card.ID > 0 {
			vm.HasClientCard = true
		}
	}
	paid, owed := journalTotals(inv.Status, details.TotalCents, details.PaidCents)
	vm.PaidLabel = moneyL(lang, paid, inv.Currency)
	vm.RemainingLabel = moneyL(lang, owed, inv.Currency)
	vm.RemainingCents = owed
	vm.Remaining = inv.Status == "sent" && owed > 0
	vm.PartialPaid = details.PaidCents > 0 && owed > 0
	vm.TodayISO = userNow(r).Format("2006-01-02")
	vm.Payments = make([]paymentVM, 0, len(details.Payments))
	reversed := map[int64]bool{}
	for _, p := range details.Payments {
		if p.Reverses > 0 {
			reversed[p.Reverses] = true
		}
	}
	for _, p := range details.Payments {
		vm.Payments = append(vm.Payments, paymentVM{
			Date: paymentDay(lang, p.PaidOn), Amount: moneyL(lang, p.AmountCents, inv.Currency),
			Note: p.Note, Source: paymentSource(lang, p.Source), ID: p.ID,
			CanReverse: p.AmountCents > 0 && p.Source != "reversal" && !reversed[p.ID],
		})
	}
	vm.Logo = logoURL(snapshot.BillingRules.Logo)
	vm.Seller = s.sellerName(r)
	if inv.Status == "void" {
		if reason, err := s.services.Invoicing.Queries.VoidReason(r.Context(), appmodel.InvoiceLookupQuery{TeamID: inv.TeamID, InvoiceID: inv.ID}); err == nil {
			vm.VoidReason = reason
		}
	}
	if inv.Status != "draft" {
		if err := s.applyIssuedSnapshot(r, inv, &vm); err != nil {
			return model.Invoice{}, invoiceVM{}, false, err
		}
	}
	if inv.VATRate > 0 {
		vm.VATIncluded = fmt.Sprintf(i18n.T(lang, "inv.vatIncluded"), inv.VATRate, moneyL(lang, vatIncludedCents(details.TotalCents, inv.VATRate), inv.Currency))
		// The QR's purpose is read by a Russian bank whatever the screen's language.
		vm.VATIncludedRU = fmt.Sprintf(i18n.T(i18n.Lang("ru"), "inv.vatIncluded"), inv.VATRate, moneyL(i18n.Lang("ru"), vatIncludedCents(details.TotalCents, inv.VATRate), inv.Currency))
	}
	return inv, vm, snapshot.StripeReady, nil
}

// invoiceOverdue marks a sent invoice that is still unpaid past its due day
// (FIN-07). A draft is not overdue: nothing has been asked of the client yet.
func invoiceOverdue(status, due, today string) bool {
	return due != "" && due < today && status != "draft" && status != "paid" && status != "void"
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

// issuedDayLabel prints the issue day of an invoice, empty for a draft: a
// draft has no issue day yet, and its creation day is not one.
func issuedDayLabel(lang i18n.Lang, issued string) string {
	day, err := time.Parse("2006-01-02", issued)
	if issued == "" || err != nil {
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
	if source == "reversal" {
		return i18n.T(lang, "inv.paySourceReversal")
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
	hide, err := s.invoiceHidesNames(r, inv)
	if err != nil {
		return nil, err
	}
	if !hide {
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

// invoiceHidesNames is whether an invoice leaves the executors' names off: as
// frozen when it was sent, or, for a draft, as the workspace switch is now.
func (s *Server) invoiceHidesNames(r *http.Request, inv model.Invoice) (bool, error) {
	frozen, err := s.services.Invoicing.Queries.HideNames(r.Context(), appmodel.InvoiceLookupQuery{TeamID: teamID(r), InvoiceID: inv.ID})
	if err != nil {
		return false, fmt.Errorf("load the invoice's name setting: %w", err)
	}
	if frozen.Frozen {
		return frozen.Hide, nil
	}
	settings, err := s.services.Teams.Settings.Settings(r.Context(), teamID(r))
	if err != nil {
		return false, fmt.Errorf("load invoice settings: %w", err)
	}
	return settings.HideExecutorNames, nil
}

// invoiceIssued is what the client was given: kept when the invoice is sent
// and used for every later copy, so the document reads as it went out. The
// payment state (paid, what is still owed, the QR) stays live.
type invoiceIssued struct {
	Seller   string                       `json:"seller"`
	Logo     string                       `json:"logo"`
	IssuedOn string                       `json:"issued_on"`
	Labels   []string                     `json:"labels"`
	Sessions []appmodel.InvoiceSessionRow `json:"sessions"`
	// Revision is the draft's revision the look was kept at: a draft edited
	// after its letter went out is kept again when it is marked sent.
	Revision int64 `json:"revision"`
}

// issuedFrom captures an invoice as it reads now, issued on the given day.
func (s *Server) issuedFrom(r *http.Request, inv model.Invoice, vm invoiceVM, issuedOn time.Time) (invoiceIssued, error) {
	issued := invoiceIssued{Seller: vm.Seller, Logo: string(vm.Logo), IssuedOn: issuedOn.Format("2006-01-02"), Revision: inv.Revision}
	for _, line := range vm.Lines {
		issued.Labels = append(issued.Labels, line.Label)
	}
	rows, err := s.liveInvoiceSessions(r, inv.ID)
	if err != nil {
		return issued, err
	}
	issued.Sessions = rows
	return issued, nil
}

// freezeIssued keeps the invoice's look as it is being issued today and
// shows the issue day on the copy at hand. It runs before the send, while the
// invoice is a draft, so the letter and every later copy agree.
func (s *Server) freezeIssued(r *http.Request, inv model.Invoice, vm *invoiceVM) error {
	today := userNow(r)
	issued, err := s.issuedFrom(r, inv, *vm, today)
	if err != nil {
		return err
	}
	data, err := json.Marshal(issued)
	if err != nil {
		return err
	}
	if err := s.services.Invoicing.Queries.SaveIssuedSnapshot(r.Context(), appmodel.InvoiceSnapshotSaveRequest{TeamID: teamID(r), InvoiceID: inv.ID, Data: string(data), IssuedOn: issued.IssuedOn}); err != nil {
		return err
	}
	vm.IssuedLabel = fmtDate(resolveLang(r), today)
	vm.IssuedISO = issued.IssuedOn
	return nil
}

// applyIssuedSnapshot reads an issued invoice as it went out. An invoice
// issued before the look was kept gets it now, dated by its creation, so its
// copies stop following later changes from here on.
func (s *Server) applyIssuedSnapshot(r *http.Request, inv model.Invoice, vm *invoiceVM) error {
	raw, err := s.services.Invoicing.Queries.IssuedSnapshot(r.Context(), appmodel.InvoiceLookupQuery{TeamID: inv.TeamID, InvoiceID: inv.ID})
	if err != nil {
		return fmt.Errorf("load the issued invoice: %w", err)
	}
	// A view never writes: the look is kept when the invoice is issued, by
	// the screen, the letter or the tool. An invoice without one reads as the
	// live document, which for a sent invoice is frozen by its own sessions.
	var issued invoiceIssued
	if raw == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), &issued); err != nil {
		return fmt.Errorf("read the issued invoice: %w", err)
	}
	vm.Seller, vm.Logo = issued.Seller, logoURL(issued.Logo)
	if day, err := time.Parse("2006-01-02", issued.IssuedOn); err == nil {
		vm.IssuedLabel = fmtDate(resolveLang(r), day)
		vm.IssuedISO = issued.IssuedOn
	}
	if len(issued.Labels) == len(vm.Lines) {
		for i := range vm.Lines {
			vm.Lines[i].Label = issued.Labels[i]
		}
	}
	return nil
}

// freezeIssuedUnlessKept keeps the look as issued today unless a letter file
// already kept it: a draft whose letter went out on Monday and is marked sent
// on Wednesday is the Monday document.
func (s *Server) freezeIssuedUnlessKept(r *http.Request, inv model.Invoice, vm *invoiceVM) error {
	if raw, err := s.services.Invoicing.Queries.IssuedSnapshot(r.Context(), appmodel.InvoiceLookupQuery{TeamID: inv.TeamID, InvoiceID: inv.ID}); err == nil && raw != "" {
		var kept invoiceIssued
		if json.Unmarshal([]byte(raw), &kept) == nil && kept.Revision == inv.Revision {
			return nil
		}
	}
	return s.freezeIssued(r, inv, vm)
}
