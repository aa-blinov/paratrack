package invoicing

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

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
		// A cancelled document is not a document: the client never owed it,
		// its hours were billed again, and its amounts would double the
		// accountant's figures.
		if inv.Status == "draft" || inv.Status == "void" || !clientMatches(inv, query) {
			continue
		}
		// The period is the documents' own dates: the day each was issued.
		issued := inv.CreatedAt
		if inv.IssuedOn != "" && (!query.From.IsZero() || !query.To.IsZero()) {
			loc := query.From.Location()
			if query.From.IsZero() {
				loc = query.To.Location()
			}
			if day, err := time.ParseInLocation("2006-01-02", inv.IssuedOn, loc); err == nil {
				issued = day
			}
		}
		if !query.From.IsZero() && issued.Before(query.From) {
			continue
		}
		if !query.To.IsZero() && !issued.Before(query.To) {
			continue
		}
		for _, line := range item.Lines {
			currency := line.Currency
			if currency == "" {
				currency = inv.Currency
			}
			out = append(out, appmodel.InvoiceExportLine{
				InvoiceID: inv.ID, IssuedOn: inv.IssuedOn,
				InvoiceNumber: inv.Number, IssuedAt: inv.CreatedAt, Client: inv.ClientName, Status: inv.Status,
				Label: line.Label, Seconds: line.Seconds, RateCents: line.RateCents, AmountCents: line.AmountCents,
				Currency: currency, UnapprovedWeeks: inv.UnapprovedWeeks,
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

// Reconcile compares the time worked for each client in a window with what the invoices
// bill for it (FIN-04).
func (s *Service) Reconcile(ctx context.Context, query appmodel.ReconcileQuery) ([]appmodel.ClientReconciliation, error) {
	if query.TeamID <= 0 || !query.Until.After(query.From) {
		return nil, ErrInvalidInvoice
	}
	rows, err := s.reader.ReconcileClients(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("reconcile clients: %w", err)
	}
	return rows, nil
}

// PaidCentsInMonth reads the payment journal over a month: the overview's
// «заработано за месяц» without a trip through the invoices section.
func (s *Service) PaidCentsInMonth(ctx context.Context, teamID int64, fromDay, toDay string) (int, string, error) {
	if teamID <= 0 {
		return 0, "", ErrInvalidInvoice
	}
	total, currency, err := s.reader.PaidCentsInMonth(ctx, teamID, fromDay, toDay)
	if err != nil {
		return 0, "", fmt.Errorf("sum paid invoices for month: %w", err)
	}
	return total, currency, nil
}

// PaymentLedger lists the payments received on the workspace's invoices, by
// the day they were paid, within the query's period (From inclusive, To
// exclusive; zero means open) and for one client when named. This is what an
// accountant sets against the bank statement: date, counterparty, ИНН, sum.
func (s *Service) PaymentLedger(ctx context.Context, query appmodel.InvoiceExportQuery) ([]appmodel.PaymentLedgerRow, error) {
	if query.TeamID <= 0 {
		return nil, ErrInvalidTeam
	}
	items, err := s.reader.ListInvoiceDetails(ctx, query.TeamID)
	if err != nil {
		return nil, fmt.Errorf("list invoices for the payment ledger: %w", err)
	}
	from, to := "", ""
	if !query.From.IsZero() {
		from = query.From.Format("2006-01-02")
	}
	if !query.To.IsZero() {
		to = query.To.Format("2006-01-02")
	}
	var out []appmodel.PaymentLedgerRow
	for _, item := range items {
		inv := item.Invoice
		if !clientMatches(inv, query) {
			continue
		}
		inn := model.ParseRequisites(inv.ClientDetails).INN
		for _, p := range item.Payments {
			if (from != "" && p.PaidOn < from) || (to != "" && p.PaidOn >= to) {
				continue
			}
			out = append(out, appmodel.PaymentLedgerRow{
				PaidOn: p.PaidOn, InvoiceNumber: inv.Number, Client: inv.ClientName, ClientINN: inn,
				AmountCents: p.AmountCents, Currency: inv.Currency, Source: p.Source, Note: p.Note,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].PaidOn != out[j].PaidOn {
			return out[i].PaidOn < out[j].PaidOn
		}
		return out[i].InvoiceNumber < out[j].InvoiceNumber
	})
	return out, nil
}

// HideNames says whether a sent invoice was frozen with the executors' names
// hidden, so the document reads as it went out.
func (s *Service) HideNames(ctx context.Context, query appmodel.InvoiceLookupQuery) (appmodel.InvoiceHideNames, error) {
	if query.TeamID <= 0 || query.InvoiceID <= 0 {
		return appmodel.InvoiceHideNames{}, ErrInvalidInvoice
	}
	return s.reader.InvoiceHideNames(ctx, query)
}

// clientMatches says whether an invoice belongs to the export's client: by
// the client card of its project, or by the name it carries, regardless of
// case and surrounding spaces.
func clientMatches(inv model.Invoice, query appmodel.InvoiceExportQuery) bool {
	if query.Client == "" {
		return true
	}
	for _, id := range query.ClientProjects {
		if inv.ProjectID == id && id > 0 {
			return true
		}
	}
	return strings.EqualFold(strings.TrimSpace(inv.ClientName), strings.TrimSpace(query.Client))
}

// IssuedSnapshot reads what an invoice looked like when it was issued.
func (s *Service) IssuedSnapshot(ctx context.Context, query appmodel.InvoiceLookupQuery) (string, error) {
	if query.TeamID <= 0 || query.InvoiceID <= 0 {
		return "", ErrInvalidInvoice
	}
	return s.reader.InvoiceIssuedSnapshot(ctx, query)
}

// SaveIssuedSnapshot keeps what an invoice looks like as it is issued.
func (s *Service) SaveIssuedSnapshot(ctx context.Context, request appmodel.InvoiceSnapshotSaveRequest) error {
	if request.TeamID <= 0 || request.InvoiceID <= 0 || request.Data == "" {
		return ErrInvalidInvoice
	}
	return s.reader.SaveInvoiceIssuedSnapshot(ctx, request)
}

// VoidReason reads why an invoice was voided.
func (s *Service) VoidReason(ctx context.Context, query appmodel.InvoiceLookupQuery) (string, error) {
	if query.TeamID <= 0 || query.InvoiceID <= 0 {
		return "", ErrInvalidInvoice
	}
	return s.reader.InvoiceVoidReason(ctx, query)
}
