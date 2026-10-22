package web

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

func (s *Server) buildInvoicesPage(r *http.Request) (invoicesPage, error) {
	snapshot, err := s.services.Invoicing.Queries.BuildIndex(r.Context(), appmodel.InvoiceIndexRequest{TeamID: teamID(r)})
	if err != nil {
		return invoicesPage{}, fmt.Errorf("build invoice index: %w", err)
	}
	lang := string(resolveLang(r))
	// Money the workspace is owed. A client says "we paid 50 000" and the
	// only way to answer is to add up what is still sent and unpaid — which
	// the list could not do, because every row carried its own amount and
	// nothing carried the sum. Grouped by currency the same way reports do,
	// so a workspace invoicing in two currencies does not get one wrong total.
	outstanding := map[string]int{}
	paid := map[string]int{}
	data := invoicesPage{
		pageData:      pageData{Title: "Invoices", Active: "invoices", Lang: lang, ReactApp: true},
		InvoicesReact: true,
		Items:         make([]invoiceSummary, 0, len(snapshot.Invoices)),
		Projects:      make([]invoiceProjectOpt, 0, len(snapshot.DraftOptions.Projects)),
		Unbilled:      make([]unbilledView, 0, len(snapshot.Unbilled)),
	}
	today := userNow(r).Format("2006-01-02")
	for _, item := range snapshot.Invoices {
		inv := item.Invoice
		switch inv.Status {
		case "paid":
			paid[inv.Currency] += item.TotalCents
		case "sent":
			outstanding[inv.Currency] += item.TotalCents
		}
		data.Items = append(data.Items, invoiceSummary{
			ID: inv.ID, Number: inv.Number, Client: inv.ClientName, Status: inv.Status,
			Total: moneyL(resolveLang(r), item.TotalCents, inv.Currency),
			Hours: fmtHoursL(resolveLang(r), item.TotalHoursHundredths),
			Period: fmtDay(resolveLang(r), inv.PeriodStart) + " – " +
				fmtDay(resolveLang(r), inv.PeriodEnd.AddDate(0, 0, -1)),
			Due: invoiceDueLabel(resolveLang(r), inv.DueDate), Overdue: invoiceOverdue(inv.Status, inv.DueDate, today),
		})
	}
	data.Outstanding = moneyByCurrency(resolveLang(r), outstanding)
	data.Paid = moneyByCurrency(resolveLang(r), paid)
	data.HasOutstanding = len(outstanding) > 0
	if settings, err := s.services.Teams.Settings.Settings(r.Context(), teamID(r)); err == nil {
		data.RoundMinutes, data.RoundMode = settings.Billing.RoundMinutes, settings.Billing.RoundMode
	}
	// Default window: this month.
	now := userNow(r)
	data.DefStart = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
	data.DefEnd = now.Format("2006-01-02")
	want, _ := strconv.ParseInt(r.URL.Query().Get("project"), 10, 64)
	if from := r.URL.Query().Get("from"); from != "" {
		if _, err := time.Parse("2006-01-02", from); err == nil {
			data.DefStart = from
		}
	}
	options := snapshot.DraftOptions
	data.Billable = options.HasBillable
	for _, projectOption := range options.Projects {
		project := projectOption.Project
		option := invoiceProjectOpt{
			ID: project.ID, Name: project.Name, Selected: project.ID == want,
			Eligible: projectOption.Eligible,
		}
		if projectOption.HasClient {
			option.ClientName = projectOption.Client.Name
			option.ClientDetails = projectOption.Client.Details
			option.ClientEmail = projectOption.Client.Email
		}
		if option.Selected {
			data.Prefill = option
		}
		data.Projects = append(data.Projects, option)
	}
	data.Unbilled = unbilledViewsFrom(snapshot.Unbilled, r)
	data.Unassigned = make([]unassignedActivityView, 0, len(snapshot.Unassigned))
	for _, activity := range snapshot.Unassigned {
		data.Unassigned = append(data.Unassigned, unassignedActivityView{
			ID: activity.ID, Sessions: activity.Sessions, Name: activity.Name, Billed: activity.Billed,
		})
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	return data, nil
}
