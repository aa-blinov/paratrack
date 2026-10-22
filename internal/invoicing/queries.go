package invoicing

import (
	"context"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
)

// ExportLines returns the lines of the issued invoices a query selects, for the
// accountant (FIN-05). A draft is not issued yet, so it stays out.
func (s *Service) ExportLines(ctx context.Context, query appmodel.InvoiceExportQuery) ([]appmodel.InvoiceExportLine, error) {
	if query.TeamID <= 0 {
		return nil, ErrInvalidTeam
	}
	items, err := s.reader.ListInvoiceDetails(ctx, query.TeamID)
	if err != nil {
		return nil, fmt.Errorf("list invoices for export: %w", err)
	}
	var out []appmodel.InvoiceExportLine
	for _, item := range items {
		inv := item.Invoice
		if inv.Status == "draft" || (query.Client != "" && inv.ClientName != query.Client) {
			continue
		}
		if !query.From.IsZero() && inv.CreatedAt.Before(query.From) {
			continue
		}
		if !query.To.IsZero() && !inv.CreatedAt.Before(query.To) {
			continue
		}
		for _, line := range item.Lines {
			currency := line.Currency
			if currency == "" {
				currency = inv.Currency
			}
			out = append(out, appmodel.InvoiceExportLine{
				InvoiceNumber: inv.Number, IssuedAt: inv.CreatedAt, Client: inv.ClientName, Status: inv.Status,
				Label: line.Label, Seconds: line.Seconds, RateCents: line.RateCents, AmountCents: line.AmountCents,
				Currency: currency,
			})
		}
	}
	return out, nil
}

func (s *Service) list(ctx context.Context, teamID int64) ([]appmodel.InvoiceSummaryResult, error) {
	if teamID <= 0 {
		return nil, ErrInvalidTeam
	}
	items, err := s.reader.ListInvoiceDetails(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("list invoices: %w", err)
	}
	summaries := make([]appmodel.InvoiceSummaryResult, 0, len(items))
	for _, item := range items {
		totalCents, totalHours, err := invoiceLineTotals(item.Lines)
		if err != nil {
			return nil, fmt.Errorf("summarize invoice %d: %w", item.Invoice.ID, err)
		}
		summaries = append(summaries, appmodel.InvoiceSummaryResult{
			Invoice: item.Invoice, TotalCents: totalCents, TotalHoursHundredths: totalHours,
			PaidCents: paidCentsOf(item.Payments),
		})
	}
	return summaries, nil
}

// BuildIndex assembles the invoice list, draft choices and billable history
// for the invoice index page.
func (s *Service) BuildIndex(ctx context.Context, request appmodel.InvoiceIndexRequest) (appmodel.InvoiceIndexSnapshot, error) {
	if request.TeamID <= 0 {
		return appmodel.InvoiceIndexSnapshot{}, ErrInvalidTeam
	}
	invoices, err := s.list(ctx, request.TeamID)
	if err != nil {
		return appmodel.InvoiceIndexSnapshot{}, fmt.Errorf("load invoice index: %w", err)
	}
	options, err := s.draftOptions(ctx, request.TeamID)
	if err != nil {
		return appmodel.InvoiceIndexSnapshot{}, fmt.Errorf("load invoice draft options: %w", err)
	}
	unbilled, err := s.UnbilledProjectTime(ctx, appmodel.UnbilledProjectQuery{TeamID: request.TeamID})
	if err != nil {
		return appmodel.InvoiceIndexSnapshot{}, fmt.Errorf("load unbilled invoice history: %w", err)
	}
	unassigned, err := s.unassignedHistory(ctx, request.TeamID)
	if err != nil {
		return appmodel.InvoiceIndexSnapshot{}, fmt.Errorf("load unassigned invoice history: %w", err)
	}
	return appmodel.InvoiceIndexSnapshot{
		Invoices: invoices, DraftOptions: options, Unbilled: unbilled, Unassigned: unassigned,
	}, nil
}

// draftOptions applies invoice eligibility rules and joins saved client details
// to the workspace's active project catalog for the invoice creation flow.
func (s *Service) draftOptions(ctx context.Context, teamID int64) (appmodel.InvoiceDraftOptions, error) {
	if teamID <= 0 {
		return appmodel.InvoiceDraftOptions{}, ErrInvalidTeam
	}
	projects, err := s.projects.ListProjects(ctx, appmodel.ProjectCatalogQuery{TeamID: teamID})
	if err != nil {
		return appmodel.InvoiceDraftOptions{}, fmt.Errorf("list invoice projects: %w", err)
	}
	clients, err := s.projects.ListProjectClients(ctx, teamID)
	if err != nil {
		return appmodel.InvoiceDraftOptions{}, fmt.Errorf("load invoice project clients: %w", err)
	}
	options := appmodel.InvoiceDraftOptions{Projects: make([]appmodel.InvoiceProjectOption, 0, len(projects))}
	for _, project := range projects {
		hasRate := project.Billable && project.BillableRateCents != nil && *project.BillableRateCents > 0
		if hasRate {
			options.HasBillable = true
		}
		option := appmodel.InvoiceProjectOption{Project: project, Eligible: !project.Archived && hasRate}
		if client, ok := clients[project.ID]; ok {
			option.Client, option.HasClient = client, true
		}
		options.Projects = append(options.Projects, option)
	}
	return options, nil
}

// UnbilledProjectTime returns billable time not yet included on an invoice.
func (s *Service) UnbilledProjectTime(ctx context.Context, query appmodel.UnbilledProjectQuery) ([]appmodel.UnbilledProject, error) {
	if query.TeamID <= 0 || (query.ProjectID != nil && *query.ProjectID <= 0) {
		return nil, ErrInvalidInvoice
	}
	items, err := s.reader.Unbilled(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list unbilled project time: %w", err)
	}
	return items, nil
}

// unassignedHistory returns activities whose historical time needs a project
// before it can be billed.
func (s *Service) unassignedHistory(ctx context.Context, teamID int64) ([]appmodel.UnassignedActivity, error) {
	if teamID <= 0 {
		return nil, ErrInvalidTeam
	}
	items, err := s.reader.UnassignedActivities(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("list unassigned invoice history: %w", err)
	}
	return items, nil
}

// overlappingDocuments finds other invoices with the same line labels in an
// overlapping period. This is advisory and does not block invoice creation.
func (s *Service) overlappingDocuments(ctx context.Context, query appmodel.InvoiceOverlapQuery) ([]string, error) {
	if query.TeamID <= 0 || query.ExcludeInvoiceID <= 0 || query.Start.IsZero() || !query.End.After(query.Start) {
		return nil, ErrInvalidInvoice
	}
	numbers, err := s.reader.OverlappingInvoices(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("find overlapping invoices: %w", err)
	}
	return numbers, nil
}

// Sessions lists the sessions billed on one invoice, for its appendix (FIN-06).
func (s *Service) Sessions(ctx context.Context, query appmodel.InvoiceLookupQuery) ([]appmodel.InvoiceSessionRow, error) {
	if query.TeamID <= 0 || query.InvoiceID <= 0 {
		return nil, ErrInvalidInvoice
	}
	rows, err := s.reader.ListInvoiceSessions(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list invoice sessions: %w", err)
	}
	return rows, nil
}

func (s *Service) Get(ctx context.Context, query appmodel.InvoiceLookupQuery) (appmodel.InvoiceDetailResult, error) {
	if query.TeamID <= 0 || query.InvoiceID <= 0 {
		return appmodel.InvoiceDetailResult{}, ErrInvalidInvoice
	}
	details, err := s.reader.GetInvoiceDetails(ctx, query)
	if err != nil {
		return appmodel.InvoiceDetailResult{}, fmt.Errorf("get invoice details: %w", err)
	}
	totalCents, totalHours, err := invoiceLineTotals(details.Lines)
	if err != nil {
		return appmodel.InvoiceDetailResult{}, fmt.Errorf("summarize invoice %d: %w", details.Invoice.ID, err)
	}
	result := appmodel.InvoiceDetailResult{
		Invoice: details.Invoice, Lines: details.Lines,
		TotalCents: totalCents, TotalHoursHundredths: totalHours,
		PaidCents: paidCentsOf(details.Payments), Payments: details.Payments,
	}
	for _, line := range details.Lines {
		result.InvoicedSeconds += line.Seconds
	}
	if details.Invoice.Status == "draft" {
		tracked, err := s.reader.TrackedSecondsForInvoice(ctx, query.TeamID, query.InvoiceID)
		if err != nil {
			return appmodel.InvoiceDetailResult{}, fmt.Errorf("sum tracked time of invoice %d: %w", query.InvoiceID, err)
		}
		result.TrackedSeconds = tracked
	}
	return result, nil
}

func invoiceLineTotals(lines []model.InvoiceLine) (int, int, error) {
	totalCents, totalHours := 0, 0
	for _, line := range lines {
		var err error
		totalCents, err = money.AddCents(totalCents, line.AmountCents)
		if err != nil {
			return 0, 0, err
		}
		totalHours, err = money.AddInt(totalHours, money.HoursHundredths(line.Seconds))
		if err != nil {
			return 0, 0, err
		}
	}
	return totalCents, totalHours, nil
}

// paidCentsOf sums an invoice's payment journal (FIN-08).
func paidCentsOf(payments []model.InvoicePayment) int {
	paid := 0
	for _, p := range payments {
		paid += p.AmountCents
	}
	return paid
}
