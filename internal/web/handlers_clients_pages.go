package web

import (
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// clientsPageData is the envelope for the /clients directory.
type clientsPageData struct {
	Title        string
	Active       string
	ReactApp     bool
	ClientsReact bool
	Clients      []clientRow
	Currencies   []currencyOption
	ShowArchived bool
	Flash        string
	FlashOK      bool
	CSRFToken    string
	Lang         string
	// Cards hold who to invoice and who may file them, so the screen does
	// not offer a "new client" button to someone whose submission would 403.
	CanManage bool
}

func (clientsPageData) isTemplateData()    {}
func (clientsPageData) usesReactApp() bool { return true }

func (p *clientsPageData) setCSRF(t string)   { p.CSRFToken = t }
func (p *clientsPageData) setLang(l string)   { p.Lang = l }
func (p clientsPageData) T(key string) string { return i18n.T(i18n.Lang(p.Lang), key) }

type clientRow struct {
	ID       int64
	Name     string
	Email    string
	Details  string
	Notes    string
	Currency string
	Archived bool
	Projects int
	// Labels are pre-formatted server-side so the table cannot disagree with
	// the duration and money formats the rest of the app uses.
	TrackedLabel  string
	BilledLabel   string
	UnbilledLabel string
}

// handleClientsList — GET /clients?archived=1
func (s *Server) handleClientsList(w http.ResponseWriter, r *http.Request) {
	tid := teamID(r)
	lang := resolveLang(r)
	showArchived := r.URL.Query().Get("archived") == "1"

	cards, err := s.services.Clients.List(r.Context(), appmodel.ClientCatalogQuery{
		TeamID: tid, IncludeArchived: showArchived,
	})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}

	// Roll the per-project numbers up to the client. Every figure here comes
	// from the same budget read the project screen uses, so a client total and
	// a project total cannot drift apart.
	projects, err := s.services.Projects.Queries.List(r.Context(), appmodel.ProjectCatalogQuery{
		TeamID: tid, IncludeArchived: true,
	})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	budgets, err := s.services.Clients.Budgets(r.Context(), appmodel.ProjectBudgetQuery{TeamID: tid})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	rollup := map[int64]*clientRollup{}
	for _, p := range projects {
		if p.ClientID <= 0 {
			continue
		}
		agg := rollup[p.ClientID]
		if agg == nil {
			agg = &clientRollup{spent: map[string]int{}, unbilled: map[string]int{}}
			rollup[p.ClientID] = agg
		}
		agg.projects++
		if b, ok := budgets[p.ID]; ok {
			agg.seconds += b.TrackedSeconds
			// Per-currency sums. Adding cents of two currencies together is
			// not a total, it is a number that means nothing: a card with one
			// dollar project and one rouble project would be labelled by
			// whichever project happened to be read first.
			currency := b.Currency
			if currency == "" {
				currency = "RUB"
			}
			agg.spent[currency] += b.SpentCents
			agg.unbilled[currency] += b.UnbilledCents
		}
	}

	rows := make([]clientRow, 0, len(cards))
	for _, c := range cards {
		row := clientRow{
			ID: c.ID, Name: c.Name, Email: c.Email, Details: c.Details,
			Notes: c.Notes, Currency: c.Currency, Archived: c.Archived,
		}
		if agg := rollup[c.ID]; agg != nil {
			row.Projects = agg.projects
			row.TrackedLabel = fmtDur(r, int(agg.seconds))
			// Invoiced money is what is left once the unbilled part is taken
			// out — the same subtraction the project screen shows, per currency.
			row.BilledLabel = moneyByCurrencyOrDash(lang, agg.billedPerCurrency(c.Currency))
			row.UnbilledLabel = moneyByCurrencyOrDash(lang, mergeCurrencies(agg.unbilled, c.Currency))
		} else {
			row.TrackedLabel = fmtDur(r, 0)
			row.BilledLabel = "—"
			row.UnbilledLabel = "—"
		}
		rows = append(rows, row)
	}

	data := clientsPageData{
		Title:        "Clients",
		Active:       "clients",
		ReactApp:     true,
		ClientsReact: true,
		Clients:      rows,
		Currencies:   currencyOptions(),
		ShowArchived: showArchived,
		CanManage:    canManage(r),
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, lang)
	}
	s.renderPageForRequest(w, r, "Clients", "clients", &data)
}

// clientRollup accumulates per-project figures for one client.
type clientRollup struct {
	projects int
	seconds  int64
	// Money is kept per currency: a customer can have a dollar retainer and a
	// rouble project, and their cents are not one number.
	spent    map[string]int
	unbilled map[string]int
}

// billedPerCurrency is what is already on an invoice: spent minus unbilled,
// currency by currency, never across.
func (r *clientRollup) billedPerCurrency(cardCurrency string) map[string]int {
	out := map[string]int{}
	for currency, spent := range r.spent {
		spent -= r.unbilled[currency]
		if spent < 0 {
			spent = 0
		}
		if spent > 0 {
			out[currency] = spent
		}
	}
	// A card currency with nothing in it still belongs in the list, otherwise
	// a client whose only project has no tracked time shows no currency at all.
	if len(out) == 0 && cardCurrency != "" {
		out[cardCurrency] = 0
	}
	return out
}

// mergeCurrencies tags a zero total with the card's own currency so an
// untouched client reads "0 ₽" for a dollar customer, not a bare dash.
func mergeCurrencies(sums map[string]int, cardCurrency string) map[string]int {
	if len(sums) == 0 && cardCurrency != "" {
		return map[string]int{cardCurrency: 0}
	}
	return sums
}

// moneyByCurrencyOrDash prints per-currency sums, or a dash when there is
// nothing at all — a zero with a currency on it would read like a real amount.
func moneyByCurrencyOrDash(lang i18n.Lang, sums map[string]int) string {
	total := 0
	for _, cents := range sums {
		total += cents
	}
	if total == 0 {
		return "—"
	}
	return moneyByCurrency(lang, sums)
}

// handleClientCreate — POST /clients
func (s *Server) handleClientCreate(w http.ResponseWriter, r *http.Request) {
	lang := resolveLang(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Redirect(w, r, "/clients?flash="+encodeFlash(false, "clients.nameRequired"), http.StatusSeeOther)
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	if email != "" {
		if _, err := mail.ParseAddress(email); err != nil {
			http.Redirect(w, r, "/clients?flash="+encodeFlash(false, "clients.invalidEmail"), http.StatusSeeOther)
			return
		}
	}
	currency := strings.TrimSpace(r.FormValue("currency"))
	// An empty code means "inherit the workspace currency", the same as on a
	// project, so only a non-empty value has to be a known currency.
	if currency != "" && !validCurrency(currency) {
		http.Redirect(w, r, "/clients?flash="+encodeFlash(false, "clients.saveFailed"), http.StatusSeeOther)
		return
	}
	_, err := s.services.Clients.Create(r.Context(), appmodel.ClientCreateRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), Name: name, Email: email,
		Details: r.FormValue("details"), Currency: currency, Notes: r.FormValue("notes"),
	})
	if err != nil {
		http.Redirect(w, r, "/clients?flash="+encodeFlash(false, s.clientFlashError(err, lang)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/clients?flash="+encodeFlash(true, "clients.created"), http.StatusSeeOther)
}

// handleClientUpdate — POST /clients/{id}
func (s *Server) handleClientUpdate(w http.ResponseWriter, r *http.Request) {
	lang := resolveLang(r)
	id, ok := clientIDFromPath(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		s.redirectClientFlash(w, r, id, encodeFlash(false, "clients.nameRequired"))
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	if email != "" {
		if _, err := mail.ParseAddress(email); err != nil {
			s.redirectClientFlash(w, r, id, encodeFlash(false, "clients.invalidEmail"))
			return
		}
	}
	currency := strings.TrimSpace(r.FormValue("currency"))
	// An empty code means "inherit the workspace currency", the same as on a
	// project, so only a non-empty value has to be a known currency.
	if currency != "" && !validCurrency(currency) {
		s.redirectClientFlash(w, r, id, encodeFlash(false, "clients.saveFailed"))
		return
	}
	_, err := s.services.Clients.Update(r.Context(), appmodel.ClientUpdateRequest{
		TeamID: teamID(r), ClientID: id, CallerID: authenticatedUserID(r),
		Update: appmodel.ClientUpdate{
			Name: name, Email: email, Details: r.FormValue("details"),
			Notes: r.FormValue("notes"), Currency: &currency,
		},
	})
	if err != nil {
		s.redirectClientFlash(w, r, id, encodeFlash(false, s.clientFlashError(err, lang)))
		return
	}
	s.redirectClientFlash(w, r, id, encodeFlash(true, "clients.updated"))
}

// handleClientArchive — POST /clients/{id}/archive
func (s *Server) handleClientArchive(w http.ResponseWriter, r *http.Request) {
	id, ok := clientIDFromPath(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	current, err := s.services.Clients.GetInTeam(r.Context(), appmodel.ClientScopeQuery{TeamID: teamID(r), ClientID: id})
	if err != nil {
		s.redirectClientFlash(w, r, id, encodeFlash(false, "clients.notFound"))
		return
	}
	_, err = s.services.Clients.Archive(r.Context(), appmodel.ClientUpdateRequest{
		TeamID: teamID(r), ClientID: id, CallerID: authenticatedUserID(r),
		Update: clientUpdatePreserving(current),
	})
	if err != nil {
		s.redirectClientFlash(w, r, id, encodeFlash(false, s.clientFlashError(err, resolveLang(r))))
		return
	}
	s.redirectClientFlash(w, r, id, encodeFlash(true, "clients.archived"))
}

// handleClientRestore — POST /clients/{id}/restore
func (s *Server) handleClientRestore(w http.ResponseWriter, r *http.Request) {
	id, ok := clientIDFromPath(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	current, err := s.services.Clients.GetInTeam(r.Context(), appmodel.ClientScopeQuery{TeamID: teamID(r), ClientID: id})
	if err != nil {
		s.redirectClientFlash(w, r, id, encodeFlash(false, "clients.notFound"))
		return
	}
	_, err = s.services.Clients.Restore(r.Context(), appmodel.ClientUpdateRequest{
		TeamID: teamID(r), ClientID: id, CallerID: authenticatedUserID(r),
		Update: clientUpdatePreserving(current),
	})
	if err != nil {
		s.redirectClientFlash(w, r, id, encodeFlash(false, s.clientFlashError(err, resolveLang(r))))
		return
	}
	s.redirectClientFlash(w, r, id, encodeFlash(true, "clients.restored"))
}

func clientUpdatePreserving(c model.Client) appmodel.ClientUpdate {
	currency := c.Currency
	return appmodel.ClientUpdate{
		Name: c.Name, Email: c.Email, Details: c.Details, Notes: c.Notes, Currency: &currency,
	}
}

func clientIDFromPath(r *http.Request) (int64, bool) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/clients/"), "/"), "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func (s *Server) redirectClientFlash(w http.ResponseWriter, r *http.Request, id int64, flash string) {
	http.Redirect(w, r, "/clients?flash="+url.QueryEscape(flash)+"#client-"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *Server) clientFlashError(err error, lang i18n.Lang) string {
	switch {
	case errors.Is(err, model.ErrAlreadyExists):
		return "clients.nameTaken"
	case errors.Is(err, model.ErrNotFound):
		return "clients.notFound"
	case errors.Is(err, model.ErrForbidden):
		return "forbidden"
	default:
		return "clients.saveFailed"
	}
}

// buildProjectExtras assembles the client link and the budget read for the
// project detail screen.
//
// A project created before client cards existed has no link; it keeps
// working because the invoice flow still falls back to the recipient snapshot
// stored on the project itself.
func (s *Server) buildProjectExtras(r *http.Request, project model.Project) (projectDetailExtras, error) {
	lang := resolveLang(r)
	team := teamID(r)
	extras := projectDetailExtras{}
	// The client directory is a separate service. A project must still open
	// when it is not wired (an older adapter, a narrowed test server), showing
	// the recipient snapshot it already had rather than a blank screen.
	if depcheck.IsNil(s.services.Clients) {
		return extras, nil
	}

	if project.ClientID > 0 {
		card, err := s.services.Clients.GetInTeam(r.Context(), appmodel.ClientScopeQuery{TeamID: team, ClientID: project.ClientID})
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return extras, err
		}
		if err == nil {
			extras.Client = &clientRef{ID: card.ID, Name: card.Name}
		}
	}
	if canManage(r) {
		cards, err := s.services.Clients.List(r.Context(), appmodel.ClientCatalogQuery{TeamID: team})
		if err != nil {
			return extras, err
		}
		options := make([]clientRef, 0, len(cards))
		for _, card := range cards {
			options = append(options, clientRef{ID: card.ID, Name: card.Name})
		}
		extras.ClientOptions = options
	}

	budgets, err := s.services.Clients.Budgets(r.Context(), appmodel.ProjectBudgetQuery{TeamID: team})
	if err != nil {
		return extras, err
	}
	b, ok := budgets[project.ID]
	if !ok {
		return extras, nil
	}
	extras.Budget.Currency = b.Currency
	extras.Budget.CurrencySourceLabel = currencySourceLabel(lang, b.CurrencySource)
	// A ceiling is measured against a rate; without one the number would be
	// zero for a reason the person did not cause, so say so explicitly.
	extras.Budget.HasRate = project.BillableRateCents != nil && *project.BillableRateCents > 0
	if canManage(r) && project.BudgetCents != nil && *project.BudgetCents > 0 {
		extras.Budget.BudgetInput = formatMoneyInput(lang, *project.BudgetCents)
	}
	extras.Budget.SpentLabel = moneyL(lang, b.SpentCents, b.Currency)
	extras.Budget.UnbilledLabel = moneyL(lang, b.UnbilledCents, b.Currency)
	if project.BudgetCents == nil || *project.BudgetCents <= 0 {
		// No ceiling set: show nothing rather than a zero that reads like
		// "you have spent your whole budget".
		return extras, nil
	}
	extras.Budget.HasBudget = true
	extras.Budget.BudgetLabel = moneyL(lang, *project.BudgetCents, b.Currency)
	extras.Budget.Percent = b.Percent
	if extras.Budget.Percent > 100 {
		extras.Budget.Percent = 100
	}
	if b.OverBudget {
		extras.Budget.Over = true
		extras.Budget.OverByLabel = moneyL(lang, b.SpentCents-*project.BudgetCents, b.Currency)
		return extras, nil
	}
	extras.Budget.RemainingLabel = moneyL(lang, *project.BudgetCents-b.SpentCents, b.Currency)
	return extras, nil
}

// clientDetailData is the envelope for /clients/{id}: one customer with the
// work and the documents that belong to them.
type clientDetailData struct {
	Title             string
	Active            string
	ReactApp          bool
	ClientDetailReact bool
	Client            clientDetailRow
	Projects          []clientProjectRow
	Invoices          []clientInvoiceRow
	Flash             string
	FlashOK           bool
	CSRFToken         string
	Lang              string
	CanManage         bool
}

func (clientDetailData) isTemplateData()    {}
func (clientDetailData) usesReactApp() bool { return true }

func (p *clientDetailData) setCSRF(t string)   { p.CSRFToken = t }
func (p *clientDetailData) setLang(l string)   { p.Lang = l }
func (p clientDetailData) T(key string) string { return i18n.T(i18n.Lang(p.Lang), key) }

type clientDetailRow struct {
	ID            int64
	Name          string
	Email         string
	Details       string
	Notes         string
	Currency      string
	Archived      bool
	TrackedLabel  string
	BilledLabel   string
	UnbilledLabel string
	BilledTotal   string
}

type clientProjectRow struct {
	Slug         string
	Name         string
	Color        string
	Archived     bool
	TrackedLabel string
	BudgetLabel  string
	SpentLabel   string
	Over         bool
}

type clientInvoiceRow struct {
	ID         int64
	Number     string
	Period     string
	Status     string
	Currency   string
	TotalLabel string
}

// handleClientDetail — GET /clients/{id}
func (s *Server) handleClientDetail(w http.ResponseWriter, r *http.Request) {
	lang := resolveLang(r)
	id, ok := clientIDFromPath(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	scope := appmodel.ClientScopeQuery{TeamID: teamID(r), ClientID: id}
	card, err := s.services.Clients.GetInTeam(r.Context(), scope)
	if errors.Is(err, model.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.writeInternalError(w, err)
		return
	}

	projects, err := s.services.Clients.Projects(r.Context(), scope)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	invoices, err := s.services.Clients.Invoices(r.Context(), scope, card.Name)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	budgets, err := s.services.Clients.Budgets(r.Context(), appmodel.ProjectBudgetQuery{TeamID: scope.TeamID})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}

	projectRows := make([]clientProjectRow, 0, len(projects))
	var seconds int64
	// Per-currency sums, same reasoning as the catalogue: a dollar retainer and
	// a rouble project are two numbers, not one.
	spentByCurrency := map[string]int{}
	unbilledByCurrency := map[string]int{}
	invoicedByCurrency := map[string]int{}
	for _, p := range projects {
		b := budgets[p.ID]
		currency := b.Currency
		if currency == "" {
			currency = "RUB"
		}
		seconds += b.TrackedSeconds
		spentByCurrency[currency] += b.SpentCents
		unbilledByCurrency[currency] += b.UnbilledCents
		row := clientProjectRow{
			Slug: p.Slug, Name: p.Name, Color: p.Color, Archived: p.Archived,
			TrackedLabel: fmtDur(r, int(b.TrackedSeconds)),
			SpentLabel:   moneyL(lang, b.SpentCents, b.Currency),
		}
		if p.BudgetCents != nil && *p.BudgetCents > 0 {
			row.BudgetLabel = moneyL(lang, *p.BudgetCents, b.Currency)
			row.Over = b.OverBudget
		}
		projectRows = append(projectRows, row)
	}
	for _, inv := range invoices {
		currency := inv.Currency
		if currency == "" {
			currency = "RUB"
		}
		invoicedByCurrency[currency] += inv.TotalCents
	}

	invoiceRows := make([]clientInvoiceRow, 0, len(invoices))
	for _, inv := range invoices {
		row := clientInvoiceRow{
			ID: inv.ID, Number: inv.Number,
			Status: inv.Status, Currency: inv.Currency,
			TotalLabel: moneyL(lang, inv.TotalCents, inv.Currency),
		}
		// Printed like every other period in the app. The end is exclusive, hence
		// the day pulled back: an invoice for 1 to 31 August reads 1 to 31 August.
		switch {
		case !inv.PeriodStart.IsZero() && !inv.PeriodEnd.IsZero():
			row.Period = fmtDate(lang, inv.PeriodStart) + " – " + fmtDate(lang, inv.PeriodEnd.AddDate(0, 0, -1))
		case !inv.PeriodStart.IsZero():
			row.Period = fmtDate(lang, inv.PeriodStart)
		}
		invoiceRows = append(invoiceRows, row)
	}

	data := clientDetailData{
		Title:             card.Name,
		Active:            "clients",
		ReactApp:          true,
		ClientDetailReact: true,
		Projects:          projectRows,
		Invoices:          invoiceRows,
		CanManage:         canManage(r),
		Client: clientDetailRow{
			ID: card.ID, Name: card.Name, Email: card.Email, Details: card.Details,
			Notes: card.Notes, Currency: card.Currency, Archived: card.Archived,
			TrackedLabel:  fmtDur(r, int(seconds)),
			BilledLabel:   moneyByCurrencyOrDash(lang, subtractPerCurrency(spentByCurrency, unbilledByCurrency)),
			UnbilledLabel: moneyByCurrencyOrDash(lang, unbilledByCurrency),
			BilledTotal:   moneyByCurrencyOrDash(lang, invoicedByCurrency),
		},
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, lang)
	}
	s.renderPageForRequest(w, r, card.Name, "clients", &data)
}

// currencySourceLabel names the rule that produced a document's currency.
func currencySourceLabel(lang i18n.Lang, source string) string {
	switch source {
	case "client":
		return i18n.T(lang, "clients.currencySource")
	case "project":
		return i18n.T(lang, "proj.currencySource")
	default:
		return i18n.T(lang, "team.currencySource")
	}
}

// subtractPerCurrency returns what is already on an invoice: spent minus
// unbilled, currency by currency.
func subtractPerCurrency(spent, unbilled map[string]int) map[string]int {
	out := map[string]int{}
	for currency, amount := range spent {
		amount -= unbilled[currency]
		if amount > 0 {
			out[currency] = amount
		}
	}
	return out
}
