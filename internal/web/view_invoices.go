package web

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/aa-blinov/paratrack/internal/money"
)

func (s *Server) buildInvoicesPage(r *http.Request) (invoicesPage, error) {
	list, err := s.services.Invoicing.Queries.List(r.Context(), teamID(r))
	if err != nil {
		return invoicesPage{}, fmt.Errorf("list invoices for page: %w", err)
	}
	lang := string(resolveLang(r))
	data := invoicesPage{
		pageData: pageData{Title: "Invoices", Active: "invoices", Lang: lang},
	}
	for _, item := range list {
		inv, lines := item.Invoice, item.Lines
		total := 0
		secs := 0
		for _, line := range lines {
			total, err = money.AddCents(total, line.AmountCents)
			if err != nil {
				return invoicesPage{}, fmt.Errorf("sum invoice %d amounts: %w", inv.ID, err)
			}
			secs, err = money.AddInt(secs, money.HoursHundredths(line.Seconds))
			if err != nil {
				return invoicesPage{}, fmt.Errorf("sum invoice %d hours: %w", inv.ID, err)
			}
		}
		data.Items = append(data.Items, invoiceSummary{
			ID: inv.ID, Number: inv.Number, Client: inv.ClientName, Status: inv.Status,
			Total: moneyL(resolveLang(r), total, inv.Currency),
			Hours: fmtHoursL(resolveLang(r), secs),
			Period: fmtDay(resolveLang(r), inv.PeriodStart) + " – " +
				fmtDay(resolveLang(r), inv.PeriodEnd.AddDate(0, 0, -1)),
		})
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
	options, err := s.services.Invoicing.Queries.DraftOptions(r.Context(), teamID(r))
	if err != nil {
		return invoicesPage{}, fmt.Errorf("load invoice draft options: %w", err)
	}
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
	data.Unbilled, err = s.unbilledViews(r, 0)
	if err != nil {
		return invoicesPage{}, fmt.Errorf("load unbilled invoice history: %w", err)
	}
	unassigned, err := s.services.Invoicing.Queries.UnassignedHistory(r.Context(), teamID(r))
	if err != nil {
		return invoicesPage{}, fmt.Errorf("load unassigned invoice history: %w", err)
	}
	data.Unassigned = make([]unassignedActivityView, 0, len(unassigned))
	for _, activity := range unassigned {
		data.Unassigned = append(data.Unassigned, unassignedActivityView{
			ID: activity.ID, Sessions: activity.Sessions, Name: activity.Name, Billed: activity.Billed,
		})
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	return data, nil
}
