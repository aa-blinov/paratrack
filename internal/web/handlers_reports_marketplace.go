package web

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/catalog"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/money"
)

// ---------------------------------------------------------------------------
// Report building, marketplace and exports.
// ---------------------------------------------------------------------------

type reportRow struct {
	Key         string // group label
	Secs        int
	Hours       string
	Rate        string
	Amount      string
	Share       float64
	AmountCents int
	RateCents   int
	RateVaries  bool
}

type reportVM struct {
	Template    catalog.ReportTemplate
	PeriodLabel string
	From, To    string
	Rows        []reportRow
	Total       string
	TotalSecs   int
	TotalAmount string
	TotalCents  int
}

// buildReport adapts request-specific labels and formatting around the
// transport-independent report aggregation workflow.
func (s *Server) buildReport(r *http.Request, tpl catalog.ReportTemplate, from, to time.Time) (reportVM, error) {
	result, err := s.services.ReportBuilder.Build(r.Context(), appmodel.ReportBuildQuery{
		TeamID: teamID(r), From: from, To: to, Now: userNow(r), Location: userLoc(r),
		GroupBy: tpl.GroupBy, Billable: tpl.Billable,
		Labels: appmodel.ReportLabels{
			Uncategorized: i18n.T(resolveLang(r), "dash.uncategorized"),
			Unassigned:    i18n.T(resolveLang(r), "report.unassigned"),
			FormerMember:  i18n.T(resolveLang(r), "report.formerMember"),
		},
	})
	if err != nil {
		return reportVM{}, err
	}
	rows := make([]reportRow, 0, len(result.Rows))
	for _, row := range result.Rows {
		key := row.Key
		if tpl.GroupBy == appmodel.ReportGroupDay {
			day := row.Day.In(userLoc(r))
			key = fmtWeekday(resolveLang(r), day) + " " + fmtDay(resolveLang(r), day)
		}
		if tpl.Billable && row.Currency != result.TeamCurrency {
			key += " (" + row.Currency + ")"
		}
		rate := moneyL(resolveLang(r), row.RateCents, row.Currency)
		if row.RateVaries {
			rate = i18n.T(resolveLang(r), "report.rateVaries")
		}
		rows = append(rows, reportRow{
			Key: key, Secs: row.Seconds, Hours: reportHours(r, tpl.Billable, row.Seconds), Share: row.Share,
			Rate: rate, Amount: moneyL(resolveLang(r), row.AmountCents, row.Currency),
			AmountCents: row.AmountCents, RateCents: row.RateCents, RateVaries: row.RateVaries,
		})
	}

	return reportVM{
		Template:    tpl,
		PeriodLabel: fmtDate(resolveLang(r), from) + " – " + fmtDate(resolveLang(r), to),
		From:        from.Format("2006-01-02"),
		To:          to.Add(-time.Second).Format("2006-01-02"),
		Rows:        rows,
		Total:       reportHours(r, tpl.Billable, result.TotalSeconds),
		TotalSecs:   result.TotalSeconds,
		TotalAmount: moneyByCurrency(resolveLang(r), result.ByCurrency),
		TotalCents:  result.TotalCents,
	}, nil
}

// handleReports lists the template gallery.
func (s *Server) handleReports(w http.ResponseWriter, r *http.Request) {
	lang := string(resolveLang(r))
	data := reportsPage{pageData: pageData{Title: "Reports", Active: "reports", Lang: lang, ReactApp: true}, ReportsReact: true}
	for _, t := range catalog.ReportTemplates() {
		data.Templates = append(data.Templates, reportCard{
			ID: t.ID, Name: t.Name, Blurb: t.Blurb, Icon: t.Icon,
		})
	}
	now := userNow(r)
	data.DefFrom = now.AddDate(0, 0, -30).Format("2006-01-02")
	data.DefTo = now.Format("2006-01-02")
	reports, err := s.loadSavedReports(r)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	data.SavedReports = savedReportViews(reports)
	data.CanManage = canManage(r)
	data.MeID = authenticatedUserID(r)
	s.renderPageForRequest(w, r, "Reports", "reports", "reports", &data)
}

type reportCard struct {
	ID    string
	Name  string
	Blurb string
	Icon  string
}

type reportsPage struct {
	pageData
	ReportsReact bool
	Templates    []reportCard
	DefFrom      string
	DefTo        string
	Flash        string
	FlashOK      bool
	// Saved reports are report objects, so their list lives here, on the screen
	// the "Отчёты" nav entry already points at. /stats keeps the "save this
	// view" action and links back to this list.
	SavedReports []savedReportView
	CanManage    bool
	MeID         int64
}

func (p *reportsPage) setCSRF(t string)  { p.pageData.setCSRF(t) }
func (p reportsPage) usesReactApp() bool { return true }

// handleReportRun renders one report. Query: id, from, to, format=html|csv.
func (s *Server) handleReportRun(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	tpl, ok := catalog.Report(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	now := userNow(r)
	from := now.AddDate(0, 0, -30)
	to := now.Add(24 * time.Hour)
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			from = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			to = t.AddDate(0, 0, 1)
		}
	}
	vm, err := s.buildReport(r, tpl, from, to)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	s.audit(r, "report.run", tpl.ID, vm.From+".."+vm.To)

	if r.URL.Query().Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+tpl.ID+`.csv"`)
		fmt.Fprintf(w, "key,hours,seconds,rate_cents,amount_cents,share\n")
		for _, row := range vm.Rows {
			rateCents := ""
			if !row.RateVaries {
				rateCents = strconv.Itoa(row.RateCents)
			}
			fmt.Fprintf(w, "%q,%s,%d,%s,%d,%.1f\n", row.Key, row.Hours, row.Secs, rateCents, row.AmountCents, row.Share)
		}
		return
	}
	lang := string(resolveLang(r))
	data := reportRunPage{pageData: pageData{Title: tpl.Name, Active: "reports", Lang: lang, ReactApp: true}, VM: vm, ReportRunReact: true}
	s.renderPageForRequest(w, r, tpl.Name, "reports", "report-run", &data)
}

type reportRunPage struct {
	pageData
	ReportRunReact bool
	VM             reportVM
}

func (p *reportRunPage) setCSRF(t string)  { p.pageData.setCSRF(t) }
func (p reportRunPage) usesReactApp() bool { return true }

// handleMarketplace renders the integration gallery.
func (s *Server) handleMarketplace(w http.ResponseWriter, r *http.Request) {
	lang := string(resolveLang(r))
	connected := map[string]bool{}
	list, err := s.services.Integrations.Queries.List(r.Context(), teamID(r))
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	for _, it := range list {
		connected[it.Provider] = true
	}
	data := marketPage{pageData: pageData{Title: "Marketplace", Active: "marketplace", Lang: lang, ReactApp: true}, MarketReact: true}
	for _, it := range catalog.Integrations() {
		data.Items = append(data.Items, marketCard{
			ID: it.ID, Name: it.Name, Category: it.Category, Icon: it.Icon,
			Blurb: it.Blurb, Available: it.Available, Connected: connected[it.ID],
			SecretHint: it.SecretHint, TargetHint: it.TargetHint,
		})
	}
	s.renderPageForRequest(w, r, "Marketplace", "marketplace", "marketplace", &data)
}

type marketCard struct {
	ID         string
	Name       string
	Category   string
	Icon       string
	Blurb      string
	Available  bool
	Connected  bool
	SecretHint string
	TargetHint string
}

type marketPage struct {
	pageData
	MarketReact bool
	Items       []marketCard
}

func (p *marketPage) setCSRF(t string)  { p.pageData.setCSRF(t) }
func (p marketPage) usesReactApp() bool { return p.MarketReact }

// reportHours: billable reports show decimal hours (what the money is
// priced from); the rest keep the "1 h 30 m" label.
func reportHours(r *http.Request, billable bool, sec int) string {
	if billable {
		return fmtHours(money.HoursHundredths(sec))
	}
	return fmtDur(r, sec)
}
