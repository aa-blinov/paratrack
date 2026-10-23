package web

import "net/http"

func (s *Server) registerPageRoutes(pages *http.ServeMux) {
	pages.HandleFunc("GET /{$}", s.mine(s.handleDashboard))
	pages.HandleFunc("GET /stats", s.handleStats)
	pages.HandleFunc("GET /export", s.handleExport)
	pages.HandleFunc("GET /export/workspace.json", s.manage(s.handleWorkspaceExport))
	pages.HandleFunc("GET /graph", s.module("graph", s.handleGraph))
	pages.HandleFunc("GET /goals", s.module("goals", s.handleGoals))
	pages.HandleFunc("GET /tasks", s.module("tasks", s.handleTasks))
	pages.HandleFunc("GET /tasks/{id}", s.module("tasks", s.handleTaskDetail))
	pages.HandleFunc("GET /tags", s.module("tags", s.handleTagsPage))
	pages.HandleFunc("GET /help", s.handleHelp)
	pages.HandleFunc("GET /access-denied", s.handleAccessDenied)

	// Timesheet and saved reports.
	pages.HandleFunc("GET /timesheet", s.mine(s.handleTimesheet))
	pages.HandleFunc("POST /api/timesheet/cell", s.mine(s.handleTimesheetCell))
	pages.HandleFunc("POST /api/timesheet/row/clear", s.mine(s.handleTimesheetRowClear))
	pages.HandleFunc("POST /api/timesheet/approve", s.manage(s.handleTimesheetApprove))
	pages.HandleFunc("POST /api/timesheet/reopen", s.manage(s.handleTimesheetReopen))
	pages.HandleFunc("POST /api/reports/save", s.handleSavedReportsCreate)
	pages.HandleFunc("POST /api/reports/{id}/delete", s.handleSavedReportsDelete)

	// API tokens and integrations.
	// The settings section has no page of its own; its first tab is the profile.
	pages.HandleFunc("GET /settings", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/settings/profile", http.StatusSeeOther)
	})
	pages.HandleFunc("GET /settings/tokens", s.handleSettingsTokens)
	pages.HandleFunc("POST /api/tokens", s.handleAPITokenCreate)
	pages.HandleFunc("POST /api/tokens/{id}/delete", s.handleAPITokenDelete)
	pages.HandleFunc("GET /integrations", s.module("integrations", s.handleIntegrations))
	pages.HandleFunc("POST /integrations", s.module("integrations", s.manage(s.handleIntegrationConnect)))
	pages.HandleFunc("GET /integrations/{id}", s.module("integrations", s.handleIntegrationDetail))
	pages.HandleFunc("POST /integrations/{id}/sync", s.module("integrations", s.manage(s.handleIntegrationSync)))
	pages.HandleFunc("POST /integrations/{id}/delete", s.module("integrations", s.manage(s.handleIntegrationDelete)))
	pages.HandleFunc("POST /integrations/start", s.module("integrations", s.mine(s.handleIntegrationStart)))

	// Project billing rates and invoices.
	pages.HandleFunc("POST /projects/{slug}/rate", s.manage(s.handleProjectRate))
	pages.HandleFunc("POST /projects/{slug}/norm", s.manage(s.handleProjectWeeklyNorm))
	pages.HandleFunc("GET /invoices", s.module("invoices", s.manage(s.handleInvoices)))
	pages.HandleFunc("GET /invoices/unapproved", s.module("invoices", s.manage(s.handleInvoiceUnapproved)))
	pages.HandleFunc("GET /invoices/export.csv", s.module("invoices", s.manage(s.handleInvoicesExportCSV)))
	pages.HandleFunc("POST /invoices", s.module("invoices", s.manage(s.handleInvoiceCreate)))
	pages.HandleFunc("POST /invoices/assign", s.module("invoices", s.manage(s.handleInvoiceAssignActivity)))
	pages.HandleFunc("GET /invoices/{id}", s.module("invoices", s.manage(s.handleInvoiceDetail)))
	pages.HandleFunc("POST /invoices/{id}/status", s.module("invoices", s.manage(s.handleInvoiceStatus)))
	pages.HandleFunc("POST /invoices/{id}/delete", s.module("invoices", s.manage(s.handleInvoiceDelete)))
	pages.HandleFunc("GET /invoices/{id}/pdf", s.module("invoices", s.manage(s.handleInvoicePDF)))
	pages.HandleFunc("GET /invoices/{id}/sessions.csv", s.module("invoices", s.manage(s.handleInvoiceSessionsCSV)))
	pages.HandleFunc("GET /invoices/{id}/act", s.module("invoices", s.manage(s.handleInvoiceAct)))
	pages.HandleFunc("POST /invoices/{id}/edit", s.module("invoices", s.manage(s.handleInvoiceEdit)))
	pages.HandleFunc("POST /invoices/{id}/rebuild", s.module("invoices", s.manage(s.handleInvoiceRebuild)))
	pages.HandleFunc("POST /invoices/{id}/receipt", s.module("invoices", s.manage(s.handleInvoiceReceipt)))
	pages.HandleFunc("POST /invoices/{id}/send", s.module("invoices", s.manage(s.handleInvoiceSend)))
	pages.HandleFunc("GET /invoices/{id}/letter.eml", s.module("invoices", s.manage(s.handleInvoiceLetter)))
	pages.HandleFunc("GET /settings/email-preview", s.manage(s.handleEmailPreview))
	pages.HandleFunc("GET /invoices/{id}/act.pdf", s.module("invoices", s.manage(s.handleInvoiceActPDF)))
	pages.HandleFunc("POST /invoices/{id}/pay", s.module("invoices", s.manage(s.handleInvoicePayLink)))
	pages.HandleFunc("POST /invoices/{id}/paid", s.module("invoices", s.manage(s.handleInvoiceMarkPaid)))
	pages.HandleFunc("POST /invoices/{id}/payments", s.module("invoices", s.manage(s.handleInvoicePayment)))
	pages.HandleFunc("POST /api/team/stripe", s.owner(s.handleTeamStripe))

	// Payroll and resource scheduling.
	pages.HandleFunc("GET /payroll", s.module("payroll", s.manage(s.handlePayroll)))
	pages.HandleFunc("POST /payroll", s.module("payroll", s.manage(s.handlePayrollCreate)))
	pages.HandleFunc("GET /payroll/{id}", s.module("payroll", s.manage(s.handlePayrollDetail)))
	pages.HandleFunc("POST /payroll/{id}/paid", s.module("payroll", s.manage(s.handlePayrollPaid)))
	pages.HandleFunc("POST /payroll/{id}/delete", s.module("payroll", s.manage(s.handlePayrollDelete)))
	pages.HandleFunc("POST /api/member/pay", s.manage(s.handleMemberPay))
	pages.HandleFunc("GET /schedule", s.module("schedule", s.handleSchedule))
	// Managers plan any project; a project's lead plans that one. The write
	// rule lives in the database, so the route only needs a signed-in member.
	pages.HandleFunc("POST /api/schedule/cell", s.module("schedule", s.handleScheduleCell))
	pages.HandleFunc("POST /api/schedule/line", s.module("schedule", s.handleScheduleLine))
	pages.HandleFunc("POST /api/schedule/line/remove", s.module("schedule", s.handleScheduleLineRemove))
	pages.HandleFunc("POST /api/schedule/copy", s.module("schedule", s.manage(s.handleScheduleCopy)))
	pages.HandleFunc("POST /api/schedule/admin", s.module("schedule", s.manage(s.handleScheduleAdmin)))

	// Integration marketplace and saved report execution.
	pages.HandleFunc("GET /integrations/marketplace", s.module("integrations", s.handleMarketplace))
	pages.HandleFunc("GET /reports", s.module("reports", s.manage(s.handleReports)))
	pages.HandleFunc("GET /reports/run", s.module("reports", s.manage(s.handleReportRun)))

	// Imports from other trackers.
	pages.HandleFunc("GET /import", s.module("import", s.handleImport))
	pages.HandleFunc("POST /import/preview", s.module("import", s.handleImportPreview))
	pages.HandleFunc("POST /import/run", s.module("import", s.handleImportRun))

	// Web Push settings.
	pages.HandleFunc("GET /settings/notifications", s.handleNotificationsPage)

	// Projects.
	pages.HandleFunc("GET /projects", s.handleProjectsList)
	pages.HandleFunc("GET /projects/new", s.manage(s.handleProjectNew))
	pages.HandleFunc("POST /projects/new", s.manage(s.handleProjectCreateForm))
	pages.HandleFunc("GET /projects/{slug}", s.handleProjectDetail)
	pages.HandleFunc("POST /projects/{slug}", s.manage(s.handleProjectUpdateForm))
	pages.HandleFunc("POST /projects/{slug}/delete", s.manage(s.handleProjectDeleteForm))

	// Clients. Reading the directory is open to every member; filing cards
	// is a manager's action, so writes sit behind s.manage.
	pages.HandleFunc("GET /clients", s.handleClientsList)
	pages.HandleFunc("GET /clients/{id}", s.handleClientDetail)
	pages.HandleFunc("POST /clients", s.manage(s.handleClientCreate))
	pages.HandleFunc("POST /clients/{id}", s.manage(s.handleClientUpdate))
	pages.HandleFunc("POST /clients/{id}/archive", s.manage(s.handleClientArchive))
	pages.HandleFunc("POST /clients/{id}/restore", s.manage(s.handleClientRestore))

	// Workspace settings, membership and profile.
	pages.HandleFunc("GET /settings/team", s.manage(s.handleTeamSettings))
	pages.HandleFunc("GET /settings/money", s.manage(s.handleTeamSettings))
	pages.HandleFunc("GET /settings/members", s.manage(s.handleTeamMembers))
	pages.HandleFunc("GET /settings/invites", s.manage(s.handleTeamInvites))
	pages.HandleFunc("GET /settings/profile", s.handleSettingsProfile)
	pages.HandleFunc("GET /settings/sections", s.manage(s.handleSectionsPage))
	pages.HandleFunc("GET /settings/preferences", s.handlePreferencesPage)
	pages.HandleFunc("GET /welcome", s.handleWelcome)

	// Public invite-accept page (auth required to actually click Join).
	pages.HandleFunc("GET /invites/{token}", s.handleInviteAcceptPage)

	// Webhook management and audit history.
	pages.HandleFunc("GET /settings/webhooks", s.owner(s.handleWebhooksPage))
	pages.HandleFunc("GET /tasks/{id}/attachments/{att}", s.module("tasks", s.handleTaskAttachmentGet))
	pages.HandleFunc("POST /api/webhooks", s.owner(s.handleWebhookCreate))
	pages.HandleFunc("POST /api/webhooks/{id}/test", s.owner(s.handleWebhookTest))
	pages.HandleFunc("POST /api/webhooks/{id}/delete", s.owner(s.handleWebhookDelete))
	pages.HandleFunc("POST /api/webhooks/{id}/secret", s.owner(s.handleWebhookRotate))
	pages.HandleFunc("GET /settings/audit", s.manage(s.handleAuditPage))

	// Page routes are mounted behind pageAuth by routes().
}
