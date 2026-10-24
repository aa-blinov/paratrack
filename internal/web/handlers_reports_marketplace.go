package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
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
	Key string // group label
	// Detail names the project and the task of an activity row, under its label (TIME-16).
	Detail      string
	Currency    string // the row's currency, for the CSV
	Secs        int
	Hours       string
	Rate        string
	Amount      string
	Share       float64
	AmountCents int
	RateCents   int
	RateVaries  bool
	// Planned is the schedule's minutes for the member over the period. It is
	// set only on the member grouping, the one that compares plan with time.
	Planned     string
	PlannedSecs int
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
	ShowPlan    bool
	TotalPlan   string
	// ShowUnlinked is set when some of the time sits on no task (TIME-05).
	ShowUnlinked    bool
	UnlinkedLabel   string
	UnlinkedPercent string
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
	showPlan := tpl.GroupBy == appmodel.ReportGroupUser
	var planned map[int64]int
	if showPlan {
		planned, err = s.services.Scheduling.PlannedMinutesByUser(r.Context(), teamID(r), from, to)
		if err != nil {
			return reportVM{}, err
		}
		// A member with a plan and no tracked time has no session, so the
		// aggregate has no row for them. That line is the one the comparison
		// exists to show, so it is added here with zero time.
		members, err := s.services.Teams.Directory.Members(r.Context(), teamID(r))
		if err != nil {
			return reportVM{}, err
		}
		seen := make(map[int64]bool, len(result.Rows))
		for _, row := range result.Rows {
			seen[row.UserID] = true
		}
		for _, member := range members {
			if seen[member.UserID] || planned[member.UserID] == 0 {
				continue
			}
			name := member.Name
			if name == "" {
				name = member.Email
			}
			result.Rows = append(result.Rows, appmodel.ReportAggregateRow{Key: name, UserID: member.UserID, Currency: result.TeamCurrency})
		}
	}
	rows := make([]reportRow, 0, len(result.Rows))
	totalPlanned := 0
	labels := appmodel.ActivityLabels(result.Rows)
	for i, row := range result.Rows {
		key := row.Key
		detail := ""
		if tpl.GroupBy == appmodel.ReportGroupDay || tpl.GroupBy == appmodel.ReportGroupDayPerson {
			day := row.Day.In(userLoc(r))
			key = fmtWeekday(resolveLang(r), day) + " " + fmtDay(resolveLang(r), day)
			if tpl.GroupBy == appmodel.ReportGroupDayPerson {
				key += " (" + row.Person + ")"
			}
		} else if tpl.GroupBy == appmodel.ReportGroupActivity {
			key = labels[i]
			detail = activityDetail(resolveLang(r), row)
		}
		if tpl.Billable && row.Currency != result.TeamCurrency {
			key += " (" + row.Currency + ")"
		}
		rate := moneyL(resolveLang(r), row.RateCents, row.Currency)
		if row.RateVaries {
			rate = i18n.T(resolveLang(r), "report.rateVaries")
		}
		item := reportRow{
			Key: key, Detail: detail, Secs: row.Seconds, Hours: reportHours(r, tpl.Billable, row.Seconds), Share: row.Share,
			Rate: rate, Amount: moneyL(resolveLang(r), row.AmountCents, row.Currency),
			AmountCents: row.AmountCents, RateCents: row.RateCents, RateVaries: row.RateVaries,
			Currency: row.Currency,
		}
		if showPlan {
			item.PlannedSecs = planned[row.UserID] * 60
			item.Planned = reportHours(r, false, item.PlannedSecs)
			totalPlanned += item.PlannedSecs
		}
		rows = append(rows, item)
	}

	// `to` is the exclusive end of the window: the builder receives it as-is
	// and the exported `To` below already steps back from it. The heading is
	// the one place that had not — it printed the boundary itself, so a run
	// for 1–11 October read "1 October – 12 October" over data that stopped
	// on the 11th. A report is the document a freelancer sends a client, so
	// the heading has to name the days the rows actually cover.
	//
	// The last covered day is derived once and used for both the heading and
	// the `To` field, so the two cannot drift apart again.
	lastDay := to.Add(-time.Second)

	return reportVM{
		Template:        tpl,
		PeriodLabel:     fmtDate(resolveLang(r), from) + " – " + fmtDate(resolveLang(r), lastDay),
		From:            from.Format("2006-01-02"),
		To:              lastDay.Format("2006-01-02"),
		Rows:            rows,
		Total:           reportHours(r, tpl.Billable, result.TotalSeconds),
		TotalSecs:       result.TotalSeconds,
		TotalAmount:     moneyByCurrency(resolveLang(r), result.ByCurrency),
		TotalCents:      result.TotalCents,
		ShowPlan:        showPlan,
		TotalPlan:       reportHours(r, false, totalPlanned),
		ShowUnlinked:    result.UnlinkedSeconds > 0,
		UnlinkedLabel:   reportHours(r, false, result.UnlinkedSeconds),
		UnlinkedPercent: unlinkedPercent(result.UnlinkedSeconds, result.TotalSeconds),
	}, nil
}

// unlinkedPercent is the share of the period's time that sits on no task.
func unlinkedPercent(unlinked, total int) string {
	if total <= 0 {
		return "0%"
	}
	return strconv.Itoa(unlinked*100/total) + "%"
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
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	reports, err := s.loadSavedReports(r)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	data.SavedReports = savedReportViews(reports)
	data.CanManage = canManage(r)
	data.MeID = authenticatedUserID(r)
	s.renderPageForRequest(w, r, "Reports", "reports", &data)
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
	if tpl.GroupBy == appmodel.ReportGroupUser {
		from, to = currentWeekWindow(r, now)
	}
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
	// A period that starts after it ends is a mistake on the form, not a server fault.
	if !to.After(from) {
		http.Redirect(w, r, "/reports?flash="+encodeFlash(false, "bad period"), http.StatusSeeOther)
		return
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
		_ = writeReportCSV(w, vm.Rows)
		return
	}
	lang := string(resolveLang(r))
	// The catalog name is English; the page title follows the person's language.
	title := tpl.Name
	if name := i18n.T(i18n.Lang(lang), "rep."+tpl.ID+".name"); name != "rep."+tpl.ID+".name" {
		title = name
	}
	data := reportRunPage{pageData: pageData{Title: title, Active: "reports", Lang: lang, ReactApp: true}, VM: vm, ReportRunReact: true}
	s.renderPageForRequest(w, r, title, "reports", &data)
}

// currentWeekWindow is the default window of a report that compares plan with
// tracked time. It covers the whole current week, so the days still to come
// count toward the plan; a window that ends today cuts the week's plan short.
func currentWeekWindow(r *http.Request, now time.Time) (from, to time.Time) {
	from = startOfWeek(r, now)
	return from, from.AddDate(0, 0, 7)
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
	s.renderPageForRequest(w, r, "Marketplace", "marketplace", &data)
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
// priced from), in the same locale as the invoice; the rest keep the
// "1 h 30 m" label.
func reportHours(r *http.Request, billable bool, sec int) string {
	if billable {
		return fmtHoursL(resolveLang(r), money.HoursHundredths(sec))
	}
	return fmtDur(r, sec)
}

// activityDetail names the project and the task an activity row holds, in one line.
func activityDetail(lang i18n.Lang, row appmodel.ReportAggregateRow) string {
	var parts []string
	if row.Project != "" {
		parts = append(parts, fmt.Sprintf(i18n.T(lang, "report.activityProject"), row.Project))
	}
	if row.Task != "" {
		parts = append(parts, fmt.Sprintf(i18n.T(lang, "report.activityTask"), row.Task))
	}
	return strings.Join(parts, ", ")
}
