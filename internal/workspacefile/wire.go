// The adapter owns the JSON names of the export file: one DTO per record, and
// the conversions to and from the shared model, which stays free of encoder tags.

package workspacefile

import (
	"github.com/aa-blinov/paratrack/internal/appmodel"
)

type ExportTeam struct {
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	Currency      string `json:"currency"`
	RoundMinutes  int    `json:"round_minutes"`
	RoundMode     string `json:"round_mode"`
	InvoicePrefix string `json:"invoice_prefix"`
	Requisites    string `json:"requisites"`
	VATNote       string `json:"vat_note"`
	Modules       string `json:"modules"`
	Logo          string `json:"logo"`
}

type ExportMember struct {
	Email           string `json:"email"`
	Name            string `json:"name"`
	Role            string `json:"role"`
	HourlyPayCents  int64  `json:"hourly_pay_cents"`
	CapacityMinutes int64  `json:"capacity_minutes"`
	AdminMinutes    int64  `json:"admin_minutes"`
	JoinedAt        string `json:"joined_at"`
}

type ExportClient struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Details   string `json:"details"`
	Currency  string `json:"currency"`
	Notes     string `json:"notes"`
	Archived  bool   `json:"archived"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type ExportProject struct {
	ID                int64  `json:"id"`
	Slug              string `json:"slug"`
	Name              string `json:"name"`
	Color             string `json:"color"`
	Archived          bool   `json:"archived"`
	ClientID          int64  `json:"client_id"`
	ClientName        string `json:"client_name"`
	ClientDetails     string `json:"client_details"`
	ClientEmail       string `json:"client_email"`
	Billable          bool   `json:"billable"`
	BillableRateCents int64  `json:"billable_rate_cents"`
	Currency          string `json:"currency"`
	BudgetCents       int64  `json:"budget_cents"`
	EstimateMinutes   int64  `json:"estimate_minutes"`
	Deadline          string `json:"deadline"`
	LeadEmail         string `json:"lead_email"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

type ExportActivity struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	ProjectID int64  `json:"project_id"`
	Archived  bool   `json:"archived"`
	Color     string `json:"color"`
	TaskID    int64  `json:"task_id"`
}

type ExportTag struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type ExportSession struct {
	ID                 int64   `json:"id"`
	UserEmail          string  `json:"user_email"`
	ActivityID         int64   `json:"activity_id"`
	StartAt            string  `json:"start_at"`
	EndAt              *string `json:"end_at"`
	Note               string  `json:"note"`
	Paused             bool    `json:"paused"`
	PausedAt           *string `json:"paused_at"`
	AccumulatedSeconds int64   `json:"accumulated_seconds"`
	LastResumeAt       *string `json:"last_resume_at"`
	TagIDs             []int64 `json:"tag_ids"`
	InvoiceID          int64   `json:"invoice_id"`
	ExternalID         string  `json:"external_id"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

type ExportGoal struct {
	ID            int64  `json:"id"`
	ActivityID    int64  `json:"activity_id"`
	Period        string `json:"period"`
	TargetMinutes int64  `json:"target_minutes"`
}

type ExportTask struct {
	ID              int64  `json:"id"`
	ProjectID       int64  `json:"project_id"`
	Title           string `json:"title"`
	Status          string `json:"status"`
	Position        int    `json:"position"`
	AssigneeEmail   string `json:"assignee_email"`
	DueDate         string `json:"due_date"`
	EstimateMinutes int    `json:"estimate_minutes"`
	CreatedByEmail  string `json:"created_by_email"`
	Archived        bool   `json:"archived"`
	ExternalRef     string `json:"external_ref"`
	ExternalURL     string `json:"external_url"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type ExportInvoice struct {
	ID            int64                  `json:"id"`
	Number        string                 `json:"number"`
	ClientName    string                 `json:"client_name"`
	ClientEmail   string                 `json:"client_email"`
	ClientDetails string                 `json:"client_details"`
	ProjectID     int64                  `json:"project_id"`
	PeriodStart   string                 `json:"period_start"`
	PeriodEnd     string                 `json:"period_end"`
	Status        string                 `json:"status"`
	Notes         string                 `json:"notes"`
	Receipt       string                 `json:"receipt"`
	PaidAt        string                 `json:"paid_at"`
	DueDate       string                 `json:"due_date"`
	Currency      string                 `json:"currency"`
	SellerDetails string                 `json:"seller_details"`
	VATNote       string                 `json:"vat_note"`
	ByPerson      bool                   `json:"by_person"`
	Revision      int64                  `json:"revision"`
	CreatedAt     string                 `json:"created_at"`
	Lines         []ExportInvoiceLine    `json:"lines"`
	Payments      []ExportInvoicePayment `json:"payments,omitempty"`
}

// ExportInvoicePayment is one entry of an invoice's payment journal in the file.
type ExportInvoicePayment struct {
	PaidOn      string `json:"paid_on"`
	AmountCents int    `json:"amount_cents"`
	Note        string `json:"note"`
	Source      string `json:"source"`
	CreatedAt   string `json:"created_at"`
}

type ExportInvoiceLine struct {
	Label       string `json:"label"`
	Detail      string `json:"detail"`
	Seconds     int64  `json:"seconds"`
	RateCents   int64  `json:"rate_cents"`
	AmountCents int64  `json:"amount_cents"`
}

type ExportPayrollRun struct {
	ID          int64               `json:"id"`
	Number      string              `json:"number"`
	PeriodStart string              `json:"period_start"`
	PeriodEnd   string              `json:"period_end"`
	Status      string              `json:"status"`
	Notes       string              `json:"notes"`
	Currency    string              `json:"currency"`
	CreatedAt   string              `json:"created_at"`
	Lines       []ExportPayrollLine `json:"lines"`
}

type ExportPayrollLine struct {
	UserEmail   string `json:"user_email"`
	Label       string `json:"label"`
	Seconds     int64  `json:"seconds"`
	RateCents   int64  `json:"rate_cents"`
	AmountCents int64  `json:"amount_cents"`
}

type ExportSchedule struct {
	UserEmail string `json:"user_email"`
	ProjectID int64  `json:"project_id"`
	Day       string `json:"day"`
	Minutes   int64  `json:"minutes"`
	Note      string `json:"note"`
	CreatedAt string `json:"created_at"`
}

type ExportScheduleRow struct {
	UserEmail string `json:"user_email"`
	ProjectID int64  `json:"project_id"`
	CreatedAt string `json:"created_at"`
}

type ExportApproval struct {
	StartsAt        string `json:"starts_at"`
	EndsAt          string `json:"ends_at"`
	ApprovedByEmail string `json:"approved_by_email"`
	ApprovedAt      string `json:"approved_at"`
}

type ExportReport struct {
	Name           string `json:"name"`
	Period         string `json:"period"`
	ProjectID      int64  `json:"project_id"`
	TagID          int64  `json:"tag_id"`
	ProjectSlug    string `json:"project_slug"`
	Tag            string `json:"tag"`
	CreatedByEmail string `json:"created_by_email"`
	CreatedAt      string `json:"created_at"`
}

type ExportWebhook struct {
	URL    string `json:"url"`
	Events string `json:"events"`
	Active bool   `json:"active"`
}

type ExportIntegration struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
	Config   string `json:"config"`
}

func encodeExportTeam(v appmodel.ExportTeam) ExportTeam {
	var out ExportTeam
	out.Logo = v.Logo
	out.Slug = v.Slug
	out.Name = v.Name
	out.Currency = v.Currency
	out.RoundMinutes = v.RoundMinutes
	out.RoundMode = v.RoundMode
	out.InvoicePrefix = v.InvoicePrefix
	out.Requisites = v.Requisites
	out.VATNote = v.VATNote
	out.Modules = v.Modules
	return out
}

func decodeExportTeam(v ExportTeam) appmodel.ExportTeam {
	var out appmodel.ExportTeam
	out.Logo = v.Logo
	out.Slug = v.Slug
	out.Name = v.Name
	out.Currency = v.Currency
	out.RoundMinutes = v.RoundMinutes
	out.RoundMode = v.RoundMode
	out.InvoicePrefix = v.InvoicePrefix
	out.Requisites = v.Requisites
	out.VATNote = v.VATNote
	out.Modules = v.Modules
	return out
}

func encodeExportMember(v appmodel.ExportMember) ExportMember {
	var out ExportMember
	out.Email = v.Email
	out.Name = v.Name
	out.Role = v.Role
	out.HourlyPayCents = v.HourlyPayCents
	out.CapacityMinutes = v.CapacityMinutes
	out.AdminMinutes = v.AdminMinutes
	out.JoinedAt = v.JoinedAt
	return out
}

func decodeExportMember(v ExportMember) appmodel.ExportMember {
	var out appmodel.ExportMember
	out.Email = v.Email
	out.Name = v.Name
	out.Role = v.Role
	out.HourlyPayCents = v.HourlyPayCents
	out.CapacityMinutes = v.CapacityMinutes
	out.AdminMinutes = v.AdminMinutes
	out.JoinedAt = v.JoinedAt
	return out
}

func encodeExportClient(v appmodel.ExportClient) ExportClient {
	var out ExportClient
	out.ID = v.ID
	out.Name = v.Name
	out.Email = v.Email
	out.Details = v.Details
	out.Currency = v.Currency
	out.Notes = v.Notes
	out.Archived = v.Archived
	out.CreatedAt = v.CreatedAt
	out.UpdatedAt = v.UpdatedAt
	return out
}

func decodeExportClient(v ExportClient) appmodel.ExportClient {
	var out appmodel.ExportClient
	out.ID = v.ID
	out.Name = v.Name
	out.Email = v.Email
	out.Details = v.Details
	out.Currency = v.Currency
	out.Notes = v.Notes
	out.Archived = v.Archived
	out.CreatedAt = v.CreatedAt
	out.UpdatedAt = v.UpdatedAt
	return out
}

func encodeExportProject(v appmodel.ExportProject) ExportProject {
	var out ExportProject
	out.ID = v.ID
	out.Slug = v.Slug
	out.Name = v.Name
	out.Color = v.Color
	out.Archived = v.Archived
	out.ClientID = v.ClientID
	out.ClientName = v.ClientName
	out.ClientDetails = v.ClientDetails
	out.ClientEmail = v.ClientEmail
	out.Billable = v.Billable
	out.BillableRateCents = v.BillableRateCents
	out.Currency = v.Currency
	out.BudgetCents = v.BudgetCents
	out.EstimateMinutes = v.EstimateMinutes
	out.Deadline = v.Deadline
	out.LeadEmail = v.LeadEmail
	out.CreatedAt = v.CreatedAt
	out.UpdatedAt = v.UpdatedAt
	return out
}

func decodeExportProject(v ExportProject) appmodel.ExportProject {
	var out appmodel.ExportProject
	out.ID = v.ID
	out.Slug = v.Slug
	out.Name = v.Name
	out.Color = v.Color
	out.Archived = v.Archived
	out.ClientID = v.ClientID
	out.ClientName = v.ClientName
	out.ClientDetails = v.ClientDetails
	out.ClientEmail = v.ClientEmail
	out.Billable = v.Billable
	out.BillableRateCents = v.BillableRateCents
	out.Currency = v.Currency
	out.BudgetCents = v.BudgetCents
	out.EstimateMinutes = v.EstimateMinutes
	out.Deadline = v.Deadline
	out.LeadEmail = v.LeadEmail
	out.CreatedAt = v.CreatedAt
	out.UpdatedAt = v.UpdatedAt
	return out
}

func encodeExportActivity(v appmodel.ExportActivity) ExportActivity {
	var out ExportActivity
	out.ID = v.ID
	out.Name = v.Name
	out.ProjectID = v.ProjectID
	out.Archived = v.Archived
	out.Color = v.Color
	out.TaskID = v.TaskID
	return out
}

func decodeExportActivity(v ExportActivity) appmodel.ExportActivity {
	var out appmodel.ExportActivity
	out.ID = v.ID
	out.Name = v.Name
	out.ProjectID = v.ProjectID
	out.Archived = v.Archived
	out.Color = v.Color
	out.TaskID = v.TaskID
	return out
}

func encodeExportTag(v appmodel.ExportTag) ExportTag {
	var out ExportTag
	out.ID = v.ID
	out.Name = v.Name
	return out
}

func decodeExportTag(v ExportTag) appmodel.ExportTag {
	var out appmodel.ExportTag
	out.ID = v.ID
	out.Name = v.Name
	return out
}

func encodeExportSession(v appmodel.ExportSession) ExportSession {
	var out ExportSession
	out.ID = v.ID
	out.UserEmail = v.UserEmail
	out.ActivityID = v.ActivityID
	out.StartAt = v.StartAt
	out.EndAt = v.EndAt
	out.Note = v.Note
	out.Paused = v.Paused
	out.PausedAt = v.PausedAt
	out.AccumulatedSeconds = v.AccumulatedSeconds
	out.LastResumeAt = v.LastResumeAt
	out.TagIDs = v.TagIDs
	out.InvoiceID = v.InvoiceID
	out.ExternalID = v.ExternalID
	out.CreatedAt = v.CreatedAt
	out.UpdatedAt = v.UpdatedAt
	return out
}

func decodeExportSession(v ExportSession) appmodel.ExportSession {
	var out appmodel.ExportSession
	out.ID = v.ID
	out.UserEmail = v.UserEmail
	out.ActivityID = v.ActivityID
	out.StartAt = v.StartAt
	out.EndAt = v.EndAt
	out.Note = v.Note
	out.Paused = v.Paused
	out.PausedAt = v.PausedAt
	out.AccumulatedSeconds = v.AccumulatedSeconds
	out.LastResumeAt = v.LastResumeAt
	out.TagIDs = v.TagIDs
	out.InvoiceID = v.InvoiceID
	out.ExternalID = v.ExternalID
	out.CreatedAt = v.CreatedAt
	out.UpdatedAt = v.UpdatedAt
	return out
}

func encodeExportGoal(v appmodel.ExportGoal) ExportGoal {
	var out ExportGoal
	out.ID = v.ID
	out.ActivityID = v.ActivityID
	out.Period = v.Period
	out.TargetMinutes = v.TargetMinutes
	return out
}

func decodeExportGoal(v ExportGoal) appmodel.ExportGoal {
	var out appmodel.ExportGoal
	out.ID = v.ID
	out.ActivityID = v.ActivityID
	out.Period = v.Period
	out.TargetMinutes = v.TargetMinutes
	return out
}

func encodeExportTask(v appmodel.ExportTask) ExportTask {
	var out ExportTask
	out.ID = v.ID
	out.ProjectID = v.ProjectID
	out.Title = v.Title
	out.Status = v.Status
	out.Position = v.Position
	out.AssigneeEmail = v.AssigneeEmail
	out.DueDate = v.DueDate
	out.EstimateMinutes = v.EstimateMinutes
	out.CreatedByEmail = v.CreatedByEmail
	out.Archived = v.Archived
	out.ExternalRef = v.ExternalRef
	out.ExternalURL = v.ExternalURL
	out.CreatedAt = v.CreatedAt
	out.UpdatedAt = v.UpdatedAt
	return out
}

func decodeExportTask(v ExportTask) appmodel.ExportTask {
	var out appmodel.ExportTask
	out.ID = v.ID
	out.ProjectID = v.ProjectID
	out.Title = v.Title
	out.Status = v.Status
	out.Position = v.Position
	out.AssigneeEmail = v.AssigneeEmail
	out.DueDate = v.DueDate
	out.EstimateMinutes = v.EstimateMinutes
	out.CreatedByEmail = v.CreatedByEmail
	out.Archived = v.Archived
	out.ExternalRef = v.ExternalRef
	out.ExternalURL = v.ExternalURL
	out.CreatedAt = v.CreatedAt
	out.UpdatedAt = v.UpdatedAt
	return out
}

func encodeExportInvoice(v appmodel.ExportInvoice) ExportInvoice {
	var out ExportInvoice
	out.ID = v.ID
	out.Number = v.Number
	out.ClientName = v.ClientName
	out.ClientEmail = v.ClientEmail
	out.ClientDetails = v.ClientDetails
	out.ProjectID = v.ProjectID
	out.PeriodStart = v.PeriodStart
	out.PeriodEnd = v.PeriodEnd
	out.Status = v.Status
	out.Notes = v.Notes
	out.Receipt = v.Receipt
	out.PaidAt = v.PaidAt
	out.DueDate = v.DueDate
	out.Currency = v.Currency
	out.SellerDetails = v.SellerDetails
	out.VATNote = v.VATNote
	out.ByPerson = v.ByPerson
	out.Revision = v.Revision
	out.CreatedAt = v.CreatedAt
	if v.Lines != nil {
		out.Lines = make([]ExportInvoiceLine, 0, len(v.Lines))
		for _, e := range v.Lines {
			out.Lines = append(out.Lines, encodeExportInvoiceLine(e))
		}
	}
	for _, p := range v.Payments {
		out.Payments = append(out.Payments, ExportInvoicePayment{PaidOn: p.PaidOn, AmountCents: p.AmountCents, Note: p.Note, Source: p.Source, CreatedAt: p.CreatedAt})
	}
	return out
}

func decodeExportInvoice(v ExportInvoice) appmodel.ExportInvoice {
	var out appmodel.ExportInvoice
	out.ID = v.ID
	out.Number = v.Number
	out.ClientName = v.ClientName
	out.ClientEmail = v.ClientEmail
	out.ClientDetails = v.ClientDetails
	out.ProjectID = v.ProjectID
	out.PeriodStart = v.PeriodStart
	out.PeriodEnd = v.PeriodEnd
	out.Status = v.Status
	out.Notes = v.Notes
	out.Receipt = v.Receipt
	out.PaidAt = v.PaidAt
	out.DueDate = v.DueDate
	out.Currency = v.Currency
	out.SellerDetails = v.SellerDetails
	out.VATNote = v.VATNote
	out.ByPerson = v.ByPerson
	out.Revision = v.Revision
	out.CreatedAt = v.CreatedAt
	if v.Lines != nil {
		out.Lines = make([]appmodel.ExportInvoiceLine, 0, len(v.Lines))
		for _, e := range v.Lines {
			out.Lines = append(out.Lines, decodeExportInvoiceLine(e))
		}
	}
	for _, p := range v.Payments {
		out.Payments = append(out.Payments, appmodel.ExportInvoicePayment{PaidOn: p.PaidOn, AmountCents: p.AmountCents, Note: p.Note, Source: p.Source, CreatedAt: p.CreatedAt})
	}
	return out
}

func encodeExportInvoiceLine(v appmodel.ExportInvoiceLine) ExportInvoiceLine {
	var out ExportInvoiceLine
	out.Label = v.Label
	out.Detail = v.Detail
	out.Seconds = v.Seconds
	out.RateCents = v.RateCents
	out.AmountCents = v.AmountCents
	return out
}

func decodeExportInvoiceLine(v ExportInvoiceLine) appmodel.ExportInvoiceLine {
	var out appmodel.ExportInvoiceLine
	out.Label = v.Label
	out.Detail = v.Detail
	out.Seconds = v.Seconds
	out.RateCents = v.RateCents
	out.AmountCents = v.AmountCents
	return out
}

func encodeExportPayrollRun(v appmodel.ExportPayrollRun) ExportPayrollRun {
	var out ExportPayrollRun
	out.ID = v.ID
	out.Number = v.Number
	out.PeriodStart = v.PeriodStart
	out.PeriodEnd = v.PeriodEnd
	out.Status = v.Status
	out.Notes = v.Notes
	out.Currency = v.Currency
	out.CreatedAt = v.CreatedAt
	if v.Lines != nil {
		out.Lines = make([]ExportPayrollLine, 0, len(v.Lines))
		for _, e := range v.Lines {
			out.Lines = append(out.Lines, encodeExportPayrollLine(e))
		}
	}
	return out
}

func decodeExportPayrollRun(v ExportPayrollRun) appmodel.ExportPayrollRun {
	var out appmodel.ExportPayrollRun
	out.ID = v.ID
	out.Number = v.Number
	out.PeriodStart = v.PeriodStart
	out.PeriodEnd = v.PeriodEnd
	out.Status = v.Status
	out.Notes = v.Notes
	out.Currency = v.Currency
	out.CreatedAt = v.CreatedAt
	if v.Lines != nil {
		out.Lines = make([]appmodel.ExportPayrollLine, 0, len(v.Lines))
		for _, e := range v.Lines {
			out.Lines = append(out.Lines, decodeExportPayrollLine(e))
		}
	}
	return out
}

func encodeExportPayrollLine(v appmodel.ExportPayrollLine) ExportPayrollLine {
	var out ExportPayrollLine
	out.UserEmail = v.UserEmail
	out.Label = v.Label
	out.Seconds = v.Seconds
	out.RateCents = v.RateCents
	out.AmountCents = v.AmountCents
	return out
}

func decodeExportPayrollLine(v ExportPayrollLine) appmodel.ExportPayrollLine {
	var out appmodel.ExportPayrollLine
	out.UserEmail = v.UserEmail
	out.Label = v.Label
	out.Seconds = v.Seconds
	out.RateCents = v.RateCents
	out.AmountCents = v.AmountCents
	return out
}

func encodeExportSchedule(v appmodel.ExportSchedule) ExportSchedule {
	var out ExportSchedule
	out.UserEmail = v.UserEmail
	out.ProjectID = v.ProjectID
	out.Day = v.Day
	out.Minutes = v.Minutes
	out.Note = v.Note
	out.CreatedAt = v.CreatedAt
	return out
}

func decodeExportSchedule(v ExportSchedule) appmodel.ExportSchedule {
	var out appmodel.ExportSchedule
	out.UserEmail = v.UserEmail
	out.ProjectID = v.ProjectID
	out.Day = v.Day
	out.Minutes = v.Minutes
	out.Note = v.Note
	out.CreatedAt = v.CreatedAt
	return out
}

func encodeExportScheduleRow(v appmodel.ExportScheduleRow) ExportScheduleRow {
	var out ExportScheduleRow
	out.UserEmail = v.UserEmail
	out.ProjectID = v.ProjectID
	out.CreatedAt = v.CreatedAt
	return out
}

func decodeExportScheduleRow(v ExportScheduleRow) appmodel.ExportScheduleRow {
	var out appmodel.ExportScheduleRow
	out.UserEmail = v.UserEmail
	out.ProjectID = v.ProjectID
	out.CreatedAt = v.CreatedAt
	return out
}

func encodeExportApproval(v appmodel.ExportApproval) ExportApproval {
	var out ExportApproval
	out.StartsAt = v.StartsAt
	out.EndsAt = v.EndsAt
	out.ApprovedByEmail = v.ApprovedByEmail
	out.ApprovedAt = v.ApprovedAt
	return out
}

func decodeExportApproval(v ExportApproval) appmodel.ExportApproval {
	var out appmodel.ExportApproval
	out.StartsAt = v.StartsAt
	out.EndsAt = v.EndsAt
	out.ApprovedByEmail = v.ApprovedByEmail
	out.ApprovedAt = v.ApprovedAt
	return out
}

func encodeExportReport(v appmodel.ExportReport) ExportReport {
	var out ExportReport
	out.Name = v.Name
	out.Period = v.Period
	out.ProjectID = v.ProjectID
	out.TagID = v.TagID
	out.ProjectSlug = v.ProjectSlug
	out.Tag = v.Tag
	out.CreatedByEmail = v.CreatedByEmail
	out.CreatedAt = v.CreatedAt
	return out
}

func decodeExportReport(v ExportReport) appmodel.ExportReport {
	var out appmodel.ExportReport
	out.Name = v.Name
	out.Period = v.Period
	out.ProjectID = v.ProjectID
	out.TagID = v.TagID
	out.ProjectSlug = v.ProjectSlug
	out.Tag = v.Tag
	out.CreatedByEmail = v.CreatedByEmail
	out.CreatedAt = v.CreatedAt
	return out
}

func encodeExportWebhook(v appmodel.ExportWebhook) ExportWebhook {
	var out ExportWebhook
	out.URL = v.URL
	out.Events = v.Events
	out.Active = v.Active
	return out
}

func decodeExportWebhook(v ExportWebhook) appmodel.ExportWebhook {
	var out appmodel.ExportWebhook
	out.URL = v.URL
	out.Events = v.Events
	out.Active = v.Active
	return out
}

func encodeExportIntegration(v appmodel.ExportIntegration) ExportIntegration {
	var out ExportIntegration
	out.Provider = v.Provider
	out.Name = v.Name
	out.Config = v.Config
	return out
}

func decodeExportIntegration(v ExportIntegration) appmodel.ExportIntegration {
	var out appmodel.ExportIntegration
	out.Provider = v.Provider
	out.Name = v.Name
	out.Config = v.Config
	return out
}

func encodeExportInvoiceLines(v []appmodel.ExportInvoiceLine) []ExportInvoiceLine {
	if v == nil {
		return nil
	}
	out := make([]ExportInvoiceLine, 0, len(v))
	for _, e := range v {
		out = append(out, encodeExportInvoiceLine(e))
	}
	return out
}

func decodeExportInvoiceLines(v []ExportInvoiceLine) []appmodel.ExportInvoiceLine {
	if v == nil {
		return nil
	}
	out := make([]appmodel.ExportInvoiceLine, 0, len(v))
	for _, e := range v {
		out = append(out, decodeExportInvoiceLine(e))
	}
	return out
}

func encodeExportPayrollLines(v []appmodel.ExportPayrollLine) []ExportPayrollLine {
	if v == nil {
		return nil
	}
	out := make([]ExportPayrollLine, 0, len(v))
	for _, e := range v {
		out = append(out, encodeExportPayrollLine(e))
	}
	return out
}

func decodeExportPayrollLines(v []ExportPayrollLine) []appmodel.ExportPayrollLine {
	if v == nil {
		return nil
	}
	out := make([]appmodel.ExportPayrollLine, 0, len(v))
	for _, e := range v {
		out = append(out, decodeExportPayrollLine(e))
	}
	return out
}
