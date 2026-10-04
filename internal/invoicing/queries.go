package invoicing

import (
	"context"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
)

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
	return appmodel.InvoiceDetailResult{
		Invoice: details.Invoice, Lines: details.Lines,
		TotalCents: totalCents, TotalHoursHundredths: totalHours,
	}, nil
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
