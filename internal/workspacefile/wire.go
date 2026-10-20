// Code generated from internal/appmodel/workspace_export.go by the export
// wire generator. The adapter owns the JSON names; the shared model stays free of
// encoder tags.

package workspacefile

import (
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

type WorkspaceExport struct {
	Format             string              `json:"format"`
	Version            int                 `json:"version"`
	ExportedAt         time.Time           `json:"exported_at"`
	Team               ExportTeam          `json:"team"`
	Members            []ExportMember      `json:"members"`
	Clients            []ExportClient      `json:"clients"`
	Projects           []ExportProject     `json:"projects"`
	Activities         []ExportActivity    `json:"activities"`
	Tags               []ExportTag         `json:"tags"`
	Sessions           []ExportSession     `json:"sessions"`
	Goals              []ExportGoal        `json:"goals"`
	Tasks              []ExportTask        `json:"tasks"`
	Invoices           []ExportInvoice     `json:"invoices"`
	PayrollRuns        []ExportPayrollRun  `json:"payroll_runs"`
	ScheduleEntries    []ExportSchedule    `json:"schedule_entries"`
	ScheduleLines      []ExportScheduleRow `json:"schedule_lines"`
	TimesheetApprovals []ExportApproval    `json:"timesheet_approvals"`
	SavedReports       []ExportReport      `json:"saved_reports"`
	Webhooks           []ExportWebhook     `json:"webhooks"`
	Integrations       []ExportIntegration `json:"integrations"`
}

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
	ID            int64               `json:"id"`
	Number        string              `json:"number"`
	ClientName    string              `json:"client_name"`
	ClientEmail   string              `json:"client_email"`
	ClientDetails string              `json:"client_details"`
	ProjectID     int64               `json:"project_id"`
	PeriodStart   string              `json:"period_start"`
	PeriodEnd     string              `json:"period_end"`
	Status        string              `json:"status"`
	Notes         string              `json:"notes"`
	Receipt       string              `json:"receipt"`
	PaidAt        string              `json:"paid_at"`
	Currency      string              `json:"currency"`
	SellerDetails string              `json:"seller_details"`
	VATNote       string              `json:"vat_note"`
	ByPerson      bool                `json:"by_person"`
	Revision      int64               `json:"revision"`
	CreatedAt     string              `json:"created_at"`
	Lines         []ExportInvoiceLine `json:"lines"`
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

func encodeWorkspaceExport(v appmodel.WorkspaceExport) WorkspaceExport {
	var out WorkspaceExport
	out.Format = v.Format
	out.Version = v.Version
	out.ExportedAt = v.ExportedAt
	out.Team = encodeExportTeam(v.Team)
	if v.Members != nil {
		out.Members = make([]ExportMember, 0, len(v.Members))
		for _, e := range v.Members {
			out.Members = append(out.Members, encodeExportMember(e))
		}
	}
	if v.Clients != nil {
		out.Clients = make([]ExportClient, 0, len(v.Clients))
		for _, e := range v.Clients {
			out.Clients = append(out.Clients, encodeExportClient(e))
		}
	}
	if v.Projects != nil {
		out.Projects = make([]ExportProject, 0, len(v.Projects))
		for _, e := range v.Projects {
			out.Projects = append(out.Projects, encodeExportProject(e))
		}
	}
	if v.Activities != nil {
		out.Activities = make([]ExportActivity, 0, len(v.Activities))
		for _, e := range v.Activities {
			out.Activities = append(out.Activities, encodeExportActivity(e))
		}
	}
	if v.Tags != nil {
		out.Tags = make([]ExportTag, 0, len(v.Tags))
		for _, e := range v.Tags {
			out.Tags = append(out.Tags, encodeExportTag(e))
		}
	}
	if v.Sessions != nil {
		out.Sessions = make([]ExportSession, 0, len(v.Sessions))
		for _, e := range v.Sessions {
			out.Sessions = append(out.Sessions, encodeExportSession(e))
		}
	}
	if v.Goals != nil {
		out.Goals = make([]ExportGoal, 0, len(v.Goals))
		for _, e := range v.Goals {
			out.Goals = append(out.Goals, encodeExportGoal(e))
		}
	}
	if v.Tasks != nil {
		out.Tasks = make([]ExportTask, 0, len(v.Tasks))
		for _, e := range v.Tasks {
			out.Tasks = append(out.Tasks, encodeExportTask(e))
		}
	}
	if v.Invoices != nil {
		out.Invoices = make([]ExportInvoice, 0, len(v.Invoices))
		for _, e := range v.Invoices {
			out.Invoices = append(out.Invoices, encodeExportInvoice(e))
		}
	}
	if v.PayrollRuns != nil {
		out.PayrollRuns = make([]ExportPayrollRun, 0, len(v.PayrollRuns))
		for _, e := range v.PayrollRuns {
			out.PayrollRuns = append(out.PayrollRuns, encodeExportPayrollRun(e))
		}
	}
	if v.ScheduleEntries != nil {
		out.ScheduleEntries = make([]ExportSchedule, 0, len(v.ScheduleEntries))
		for _, e := range v.ScheduleEntries {
			out.ScheduleEntries = append(out.ScheduleEntries, encodeExportSchedule(e))
		}
	}
	if v.ScheduleLines != nil {
		out.ScheduleLines = make([]ExportScheduleRow, 0, len(v.ScheduleLines))
		for _, e := range v.ScheduleLines {
			out.ScheduleLines = append(out.ScheduleLines, encodeExportScheduleRow(e))
		}
	}
	if v.TimesheetApprovals != nil {
		out.TimesheetApprovals = make([]ExportApproval, 0, len(v.TimesheetApprovals))
		for _, e := range v.TimesheetApprovals {
			out.TimesheetApprovals = append(out.TimesheetApprovals, encodeExportApproval(e))
		}
	}
	if v.SavedReports != nil {
		out.SavedReports = make([]ExportReport, 0, len(v.SavedReports))
		for _, e := range v.SavedReports {
			out.SavedReports = append(out.SavedReports, encodeExportReport(e))
		}
	}
	if v.Webhooks != nil {
		out.Webhooks = make([]ExportWebhook, 0, len(v.Webhooks))
		for _, e := range v.Webhooks {
			out.Webhooks = append(out.Webhooks, encodeExportWebhook(e))
		}
	}
	if v.Integrations != nil {
		out.Integrations = make([]ExportIntegration, 0, len(v.Integrations))
		for _, e := range v.Integrations {
			out.Integrations = append(out.Integrations, encodeExportIntegration(e))
		}
	}
	return out
}

func decodeWorkspaceExport(v WorkspaceExport) appmodel.WorkspaceExport {
	var out appmodel.WorkspaceExport
	out.Format = v.Format
	out.Version = v.Version
	out.ExportedAt = v.ExportedAt
	out.Team = decodeExportTeam(v.Team)
	if v.Members != nil {
		out.Members = make([]appmodel.ExportMember, 0, len(v.Members))
		for _, e := range v.Members {
			out.Members = append(out.Members, decodeExportMember(e))
		}
	}
	if v.Clients != nil {
		out.Clients = make([]appmodel.ExportClient, 0, len(v.Clients))
		for _, e := range v.Clients {
			out.Clients = append(out.Clients, decodeExportClient(e))
		}
	}
	if v.Projects != nil {
		out.Projects = make([]appmodel.ExportProject, 0, len(v.Projects))
		for _, e := range v.Projects {
			out.Projects = append(out.Projects, decodeExportProject(e))
		}
	}
	if v.Activities != nil {
		out.Activities = make([]appmodel.ExportActivity, 0, len(v.Activities))
		for _, e := range v.Activities {
			out.Activities = append(out.Activities, decodeExportActivity(e))
		}
	}
	if v.Tags != nil {
		out.Tags = make([]appmodel.ExportTag, 0, len(v.Tags))
		for _, e := range v.Tags {
			out.Tags = append(out.Tags, decodeExportTag(e))
		}
	}
	if v.Sessions != nil {
		out.Sessions = make([]appmodel.ExportSession, 0, len(v.Sessions))
		for _, e := range v.Sessions {
			out.Sessions = append(out.Sessions, decodeExportSession(e))
		}
	}
	if v.Goals != nil {
		out.Goals = make([]appmodel.ExportGoal, 0, len(v.Goals))
		for _, e := range v.Goals {
			out.Goals = append(out.Goals, decodeExportGoal(e))
		}
	}
	if v.Tasks != nil {
		out.Tasks = make([]appmodel.ExportTask, 0, len(v.Tasks))
		for _, e := range v.Tasks {
			out.Tasks = append(out.Tasks, decodeExportTask(e))
		}
	}
	if v.Invoices != nil {
		out.Invoices = make([]appmodel.ExportInvoice, 0, len(v.Invoices))
		for _, e := range v.Invoices {
			out.Invoices = append(out.Invoices, decodeExportInvoice(e))
		}
	}
	if v.PayrollRuns != nil {
		out.PayrollRuns = make([]appmodel.ExportPayrollRun, 0, len(v.PayrollRuns))
		for _, e := range v.PayrollRuns {
			out.PayrollRuns = append(out.PayrollRuns, decodeExportPayrollRun(e))
		}
	}
	if v.ScheduleEntries != nil {
		out.ScheduleEntries = make([]appmodel.ExportSchedule, 0, len(v.ScheduleEntries))
		for _, e := range v.ScheduleEntries {
			out.ScheduleEntries = append(out.ScheduleEntries, decodeExportSchedule(e))
		}
	}
	if v.ScheduleLines != nil {
		out.ScheduleLines = make([]appmodel.ExportScheduleRow, 0, len(v.ScheduleLines))
		for _, e := range v.ScheduleLines {
			out.ScheduleLines = append(out.ScheduleLines, decodeExportScheduleRow(e))
		}
	}
	if v.TimesheetApprovals != nil {
		out.TimesheetApprovals = make([]appmodel.ExportApproval, 0, len(v.TimesheetApprovals))
		for _, e := range v.TimesheetApprovals {
			out.TimesheetApprovals = append(out.TimesheetApprovals, decodeExportApproval(e))
		}
	}
	if v.SavedReports != nil {
		out.SavedReports = make([]appmodel.ExportReport, 0, len(v.SavedReports))
		for _, e := range v.SavedReports {
			out.SavedReports = append(out.SavedReports, decodeExportReport(e))
		}
	}
	if v.Webhooks != nil {
		out.Webhooks = make([]appmodel.ExportWebhook, 0, len(v.Webhooks))
		for _, e := range v.Webhooks {
			out.Webhooks = append(out.Webhooks, decodeExportWebhook(e))
		}
	}
	if v.Integrations != nil {
		out.Integrations = make([]appmodel.ExportIntegration, 0, len(v.Integrations))
		for _, e := range v.Integrations {
			out.Integrations = append(out.Integrations, decodeExportIntegration(e))
		}
	}
	return out
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

func encodeWorkspaceExports(v []appmodel.WorkspaceExport) []WorkspaceExport {
	if v == nil {
		return nil
	}
	out := make([]WorkspaceExport, 0, len(v))
	for _, e := range v {
		out = append(out, encodeWorkspaceExport(e))
	}
	return out
}

func decodeWorkspaceExports(v []WorkspaceExport) []appmodel.WorkspaceExport {
	if v == nil {
		return nil
	}
	out := make([]appmodel.WorkspaceExport, 0, len(v))
	for _, e := range v {
		out = append(out, decodeWorkspaceExport(e))
	}
	return out
}

func encodeExportTeams(v []appmodel.ExportTeam) []ExportTeam {
	if v == nil {
		return nil
	}
	out := make([]ExportTeam, 0, len(v))
	for _, e := range v {
		out = append(out, encodeExportTeam(e))
	}
	return out
}

func decodeExportTeams(v []ExportTeam) []appmodel.ExportTeam {
	if v == nil {
		return nil
	}
	out := make([]appmodel.ExportTeam, 0, len(v))
	for _, e := range v {
		out = append(out, decodeExportTeam(e))
	}
	return out
}

func encodeExportMembers(v []appmodel.ExportMember) []ExportMember {
	if v == nil {
		return nil
	}
	out := make([]ExportMember, 0, len(v))
	for _, e := range v {
		out = append(out, encodeExportMember(e))
	}
	return out
}

func decodeExportMembers(v []ExportMember) []appmodel.ExportMember {
	if v == nil {
		return nil
	}
	out := make([]appmodel.ExportMember, 0, len(v))
	for _, e := range v {
		out = append(out, decodeExportMember(e))
	}
	return out
}

func encodeExportClients(v []appmodel.ExportClient) []ExportClient {
	if v == nil {
		return nil
	}
	out := make([]ExportClient, 0, len(v))
	for _, e := range v {
		out = append(out, encodeExportClient(e))
	}
	return out
}

func decodeExportClients(v []ExportClient) []appmodel.ExportClient {
	if v == nil {
		return nil
	}
	out := make([]appmodel.ExportClient, 0, len(v))
	for _, e := range v {
		out = append(out, decodeExportClient(e))
	}
	return out
}

func encodeExportProjects(v []appmodel.ExportProject) []ExportProject {
	if v == nil {
		return nil
	}
	out := make([]ExportProject, 0, len(v))
	for _, e := range v {
		out = append(out, encodeExportProject(e))
	}
	return out
}

func decodeExportProjects(v []ExportProject) []appmodel.ExportProject {
	if v == nil {
		return nil
	}
	out := make([]appmodel.ExportProject, 0, len(v))
	for _, e := range v {
		out = append(out, decodeExportProject(e))
	}
	return out
}

func encodeExportActivitys(v []appmodel.ExportActivity) []ExportActivity {
	if v == nil {
		return nil
	}
	out := make([]ExportActivity, 0, len(v))
	for _, e := range v {
		out = append(out, encodeExportActivity(e))
	}
	return out
}

func decodeExportActivitys(v []ExportActivity) []appmodel.ExportActivity {
	if v == nil {
		return nil
	}
	out := make([]appmodel.ExportActivity, 0, len(v))
	for _, e := range v {
		out = append(out, decodeExportActivity(e))
	}
	return out
}

func encodeExportTags(v []appmodel.ExportTag) []ExportTag {
	if v == nil {
		return nil
	}
	out := make([]ExportTag, 0, len(v))
	for _, e := range v {
		out = append(out, encodeExportTag(e))
	}
	return out
}

func decodeExportTags(v []ExportTag) []appmodel.ExportTag {
	if v == nil {
		return nil
	}
	out := make([]appmodel.ExportTag, 0, len(v))
	for _, e := range v {
		out = append(out, decodeExportTag(e))
	}
	return out
}

func encodeExportSessions(v []appmodel.ExportSession) []ExportSession {
	if v == nil {
		return nil
	}
	out := make([]ExportSession, 0, len(v))
	for _, e := range v {
		out = append(out, encodeExportSession(e))
	}
	return out
}

func decodeExportSessions(v []ExportSession) []appmodel.ExportSession {
	if v == nil {
		return nil
	}
	out := make([]appmodel.ExportSession, 0, len(v))
	for _, e := range v {
		out = append(out, decodeExportSession(e))
	}
	return out
}

func encodeExportGoals(v []appmodel.ExportGoal) []ExportGoal {
	if v == nil {
		return nil
	}
	out := make([]ExportGoal, 0, len(v))
	for _, e := range v {
		out = append(out, encodeExportGoal(e))
	}
	return out
}

func decodeExportGoals(v []ExportGoal) []appmodel.ExportGoal {
	if v == nil {
		return nil
	}
	out := make([]appmodel.ExportGoal, 0, len(v))
	for _, e := range v {
		out = append(out, decodeExportGoal(e))
	}
	return out
}

func encodeExportTasks(v []appmodel.ExportTask) []ExportTask {
	if v == nil {
		return nil
	}
	out := make([]ExportTask, 0, len(v))
	for _, e := range v {
		out = append(out, encodeExportTask(e))
	}
	return out
}

func decodeExportTasks(v []ExportTask) []appmodel.ExportTask {
	if v == nil {
		return nil
	}
	out := make([]appmodel.ExportTask, 0, len(v))
	for _, e := range v {
		out = append(out, decodeExportTask(e))
	}
	return out
}

func encodeExportInvoices(v []appmodel.ExportInvoice) []ExportInvoice {
	if v == nil {
		return nil
	}
	out := make([]ExportInvoice, 0, len(v))
	for _, e := range v {
		out = append(out, encodeExportInvoice(e))
	}
	return out
}

func decodeExportInvoices(v []ExportInvoice) []appmodel.ExportInvoice {
	if v == nil {
		return nil
	}
	out := make([]appmodel.ExportInvoice, 0, len(v))
	for _, e := range v {
		out = append(out, decodeExportInvoice(e))
	}
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

func encodeExportPayrollRuns(v []appmodel.ExportPayrollRun) []ExportPayrollRun {
	if v == nil {
		return nil
	}
	out := make([]ExportPayrollRun, 0, len(v))
	for _, e := range v {
		out = append(out, encodeExportPayrollRun(e))
	}
	return out
}

func decodeExportPayrollRuns(v []ExportPayrollRun) []appmodel.ExportPayrollRun {
	if v == nil {
		return nil
	}
	out := make([]appmodel.ExportPayrollRun, 0, len(v))
	for _, e := range v {
		out = append(out, decodeExportPayrollRun(e))
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

func encodeExportSchedules(v []appmodel.ExportSchedule) []ExportSchedule {
	if v == nil {
		return nil
	}
	out := make([]ExportSchedule, 0, len(v))
	for _, e := range v {
		out = append(out, encodeExportSchedule(e))
	}
	return out
}

func decodeExportSchedules(v []ExportSchedule) []appmodel.ExportSchedule {
	if v == nil {
		return nil
	}
	out := make([]appmodel.ExportSchedule, 0, len(v))
	for _, e := range v {
		out = append(out, decodeExportSchedule(e))
	}
	return out
}

func encodeExportScheduleRows(v []appmodel.ExportScheduleRow) []ExportScheduleRow {
	if v == nil {
		return nil
	}
	out := make([]ExportScheduleRow, 0, len(v))
	for _, e := range v {
		out = append(out, encodeExportScheduleRow(e))
	}
	return out
}

func decodeExportScheduleRows(v []ExportScheduleRow) []appmodel.ExportScheduleRow {
	if v == nil {
		return nil
	}
	out := make([]appmodel.ExportScheduleRow, 0, len(v))
	for _, e := range v {
		out = append(out, decodeExportScheduleRow(e))
	}
	return out
}

func encodeExportApprovals(v []appmodel.ExportApproval) []ExportApproval {
	if v == nil {
		return nil
	}
	out := make([]ExportApproval, 0, len(v))
	for _, e := range v {
		out = append(out, encodeExportApproval(e))
	}
	return out
}

func decodeExportApprovals(v []ExportApproval) []appmodel.ExportApproval {
	if v == nil {
		return nil
	}
	out := make([]appmodel.ExportApproval, 0, len(v))
	for _, e := range v {
		out = append(out, decodeExportApproval(e))
	}
	return out
}

func encodeExportReports(v []appmodel.ExportReport) []ExportReport {
	if v == nil {
		return nil
	}
	out := make([]ExportReport, 0, len(v))
	for _, e := range v {
		out = append(out, encodeExportReport(e))
	}
	return out
}

func decodeExportReports(v []ExportReport) []appmodel.ExportReport {
	if v == nil {
		return nil
	}
	out := make([]appmodel.ExportReport, 0, len(v))
	for _, e := range v {
		out = append(out, decodeExportReport(e))
	}
	return out
}

func encodeExportWebhooks(v []appmodel.ExportWebhook) []ExportWebhook {
	if v == nil {
		return nil
	}
	out := make([]ExportWebhook, 0, len(v))
	for _, e := range v {
		out = append(out, encodeExportWebhook(e))
	}
	return out
}

func decodeExportWebhooks(v []ExportWebhook) []appmodel.ExportWebhook {
	if v == nil {
		return nil
	}
	out := make([]appmodel.ExportWebhook, 0, len(v))
	for _, e := range v {
		out = append(out, decodeExportWebhook(e))
	}
	return out
}

func encodeExportIntegrations(v []appmodel.ExportIntegration) []ExportIntegration {
	if v == nil {
		return nil
	}
	out := make([]ExportIntegration, 0, len(v))
	for _, e := range v {
		out = append(out, encodeExportIntegration(e))
	}
	return out
}

func decodeExportIntegrations(v []ExportIntegration) []appmodel.ExportIntegration {
	if v == nil {
		return nil
	}
	out := make([]appmodel.ExportIntegration, 0, len(v))
	for _, e := range v {
		out = append(out, decodeExportIntegration(e))
	}
	return out
}
