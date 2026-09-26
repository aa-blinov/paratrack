package web

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/aa-blinov/paratrack/internal/catalog"
)

// ---------------------------------------------------------------------------
// Wave 7: report templates (grouped breakdowns + export)
// ---------------------------------------------------------------------------

type reportRow struct {
	Key     string  // group label
	Secs    int
	Hours   string
	Rate    string
	Amount  string
	Share   float64
	AmountCents int
	RateCents   int
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

// buildReport aggregates sessions in [from,to) per the template's
// groupBy, optionally pricing by the project rate.
func (s *Server) buildReport(r *http.Request, tpl catalog.ReportTemplate, from, to time.Time) (reportVM, error) {
	now := time.Now()
	list, err := s.db.ListClosedSessionsInRange(r.Context(), teamID(r), from, to, nil)
	if err != nil {
		return reportVM{}, err
	}

	// user names for utilization
	userName := map[int64]string{}
	if tpl.GroupBy == "user" {
		if u, ok := UserFrom(r.Context()); ok {
			userName[u.ID] = u.Name
		}
	}

	type acc struct {
		secs   int
		rate   int
		amount int
	}
	buckets := map[string]*acc{}
	total := 0
	totalCents := 0

	for _, as := range list {
		sec := as.Session.TrackedSecondsInWindow(from, to, now)
		if sec <= 0 {
			continue
		}
		key := ""
		switch tpl.GroupBy {
		case "project":
			if as.Activity.ProjectID == 0 {
				key = "Uncategorized"
			} else {
				if p, err := s.db.GetProject(r.Context(), as.Activity.ProjectID); err == nil {
					key = p.Name
					if tpl.Billable && p.BillableRateCents != nil {
						_ = p
					}
				} else {
					key = "Uncategorized"
				}
			}
		case "activity":
			key = as.Activity.Name
		case "day":
			key = as.Session.StartAt.Local().Format("Mon Jan 2")
		case "user":
			uid := as.Session.UserID
			if uid == 0 {
				key = "Unassigned"
			} else if n, ok := userName[uid]; ok {
				key = n
			} else {
				key = fmt.Sprintf("user %d", uid)
			}
		default:
			key = as.Activity.Name
		}
		a := buckets[key]
		if a == nil {
			a = &acc{}
			buckets[key] = a
		}
		a.secs += sec
		total += sec
		if tpl.Billable {
			rate := 0
			if as.Activity.ProjectID > 0 {
				if p, err := s.db.GetProject(r.Context(), as.Activity.ProjectID); err == nil &&
					p.Billable && p.BillableRateCents != nil {
					rate = *p.BillableRateCents
				}
			}
			a.rate = rate
			amt := sec * rate / 3600
			a.amount += amt
			totalCents += amt
		}
	}

	rows := make([]reportRow, 0, len(buckets))
	for k, a := range buckets {
		share := 0.0
		if total > 0 {
			share = float64(a.secs) / float64(total) * 100
		}
		rows = append(rows, reportRow{
			Key: k, Secs: a.secs, Hours: fmtDuration(a.secs), Share: share,
			Rate: formatMoney(a.rate), Amount: formatMoney(a.amount),
			AmountCents: a.amount, RateCents: a.rate,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Secs > rows[j].Secs })

	return reportVM{
		Template:    tpl,
		PeriodLabel: from.Format("Jan 2, 2006") + " – " + to.Format("Jan 2, 2006"),
		From:        from.Format("2006-01-02"),
		To:          to.Add(-time.Second).Format("2006-01-02"),
		Rows:        rows,
		Total:       fmtDuration(total),
		TotalSecs:   total,
		TotalAmount: formatMoney(totalCents),
		TotalCents:  totalCents,
	}, nil
}

// handleReports lists the template gallery.
func (s *Server) handleReports(w http.ResponseWriter, r *http.Request) {
	lang := string(resolveLang(r))
	data := reportsPage{pageData: pageData{Title: "Reports", Active: "reports", Lang: lang}}
	for _, t := range catalog.ReportTemplates() {
		data.Templates = append(data.Templates, reportCard{
			ID: t.ID, Name: t.Name, Blurb: t.Blurb, Icon: t.Icon,
		})
	}
	now := time.Now()
	data.DefFrom = now.AddDate(0, 0, -30).Format("2006-01-02")
	data.DefTo = now.Format("2006-01-02")
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
	Templates []reportCard
	DefFrom   string
	DefTo     string
	Flash     string
	FlashOK   bool
}

func (p *reportsPage) setCSRF(t string) { p.pageData.setCSRF(t) }

// handleReportRun renders one report. Query: id, from, to, format=html|csv.
func (s *Server) handleReportRun(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	tpl, ok := catalog.Report(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	now := time.Now()
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
		http.Error(w, err.Error(), 500)
		return
	}
	s.audit(r, "report.run", tpl.ID, vm.From+".."+vm.To)

	if r.URL.Query().Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+tpl.ID+`.csv"`)
		fmt.Fprintf(w, "key,hours,seconds,rate_cents,amount_cents,share\n")
		for _, row := range vm.Rows {
			fmt.Fprintf(w, "%q,%s,%d,%d,%d,%.1f\n", row.Key, row.Hours, row.Secs, row.RateCents, row.AmountCents, row.Share)
		}
		return
	}
	lang := string(resolveLang(r))
	data := reportRunPage{pageData: pageData{Title: tpl.Name, Active: "reports", Lang: lang}, VM: vm}
	s.renderPageForRequest(w, r, tpl.Name, "reports", "report-run", &data)
}

type reportRunPage struct {
	pageData
	VM reportVM
}

func (p *reportRunPage) setCSRF(t string) { p.pageData.setCSRF(t) }

// handleMarketplace renders the integration gallery.
func (s *Server) handleMarketplace(w http.ResponseWriter, r *http.Request) {
	lang := string(resolveLang(r))
	connected := map[string]bool{}
	if list, err := s.db.ListIntegrations(r.Context(), teamID(r)); err == nil {
		for _, it := range list {
			connected[it.Provider] = true
		}
	}
	data := marketPage{pageData: pageData{Title: "Marketplace", Active: "marketplace", Lang: lang}}
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
	Items []marketCard
}

func (p *marketPage) setCSRF(t string) { p.pageData.setCSRF(t) }
