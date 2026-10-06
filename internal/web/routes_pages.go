package web

import "net/http"

func (s *Server) registerPageRoutes(pages *http.ServeMux) {
	pages.HandleFunc("GET /{$}", s.mine(s.handleDashboard))
	pages.HandleFunc("GET /stats", s.handleStats)
	pages.HandleFunc("GET /export", s.handleExport)
	pages.HandleFunc("GET /graph", s.module("graph", s.handleGraph))
	pages.HandleFunc("GET /goals", s.module("goals", s.handleGoals))
	pages.HandleFunc("GET /tags", s.module("tags", s.handleTagsPage))
	pages.HandleFunc("GET /help", s.handleHelp)
	pages.HandleFunc("GET /tags-list-fragment", s.handleTagsFragment)

	// Timesheet and saved reports.
	pages.HandleFunc("GET /timesheet", s.mine(s.handleTimesheet))
	pages.HandleFunc("POST /api/timesheet/cell", s.mine(s.handleTimesheetCell))
	pages.HandleFunc("POST /api/timesheet/row/clear", s.mine(s.handleTimesheetRowClear))
	pages.HandleFunc("POST /api/reports/save", s.handleSavedReportsCreate)
	pages.HandleFunc("POST /api/reports/{id}/delete", s.handleSavedReportsDelete)

	// API tokens and integrations.
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
	pages.HandleFunc("GET /invoices", s.module("invoices", s.manage(s.handleInvoices)))
	pages.HandleFunc("POST /invoices", s.module("invoices", s.manage(s.handleInvoiceCreate)))
	pages.HandleFunc("POST /invoices/assign", s.module("invoices", s.manage(s.handleInvoiceAssignActivity)))
	pages.HandleFunc("GET /invoices/{id}", s.module("invoices", s.manage(s.handleInvoiceDetail)))
	pages.HandleFunc("POST /invoices/{id}/status", s.module("invoices", s.manage(s.handleInvoiceStatus)))
	pages.HandleFunc("POST /invoices/{id}/delete", s.module("invoices", s.manage(s.handleInvoiceDelete)))
	pages.HandleFunc("GET /invoices/{id}/pdf", s.module("invoices", s.manage(s.handleInvoicePDF)))
	pages.HandleFunc("GET /invoices/{id}/act", s.module("invoices", s.manage(s.handleInvoiceAct)))
	pages.HandleFunc("POST /invoices/{id}/edit", s.module("invoices", s.manage(s.handleInvoiceEdit)))
	pages.HandleFunc("POST /invoices/{id}/rebuild", s.module("invoices", s.manage(s.handleInvoiceRebuild)))
	pages.HandleFunc("POST /invoices/{id}/receipt", s.module("invoices", s.manage(s.handleInvoiceReceipt)))
	pages.HandleFunc("POST /invoices/{id}/send", s.module("invoices", s.manage(s.handleInvoiceSend)))
	pages.HandleFunc("GET /settings/email-preview", s.manage(s.handleEmailPreview))
	pages.HandleFunc("GET /invoices/{id}/act.pdf", s.module("invoices", s.manage(s.handleInvoiceActPDF)))
	pages.HandleFunc("POST /invoices/{id}/pay", s.module("invoices", s.manage(s.handleInvoicePayLink)))
	pages.HandleFunc("POST /invoices/{id}/paid", s.module("invoices", s.manage(s.handleInvoiceMarkPaid)))
	pages.HandleFunc("POST /api/team/stripe", s.manage(s.handleTeamStripe))

	// Payroll and resource scheduling.
	pages.HandleFunc("GET /payroll", s.module("payroll", s.manage(s.handlePayroll)))
	pages.HandleFunc("POST /payroll", s.module("payroll", s.manage(s.handlePayrollCreate)))
	pages.HandleFunc("GET /payroll/{id}", s.module("payroll", s.manage(s.handlePayrollDetail)))
	pages.HandleFunc("POST /payroll/{id}/paid", s.module("payroll", s.manage(s.handlePayrollPaid)))
	pages.HandleFunc("POST /payroll/{id}/delete", s.module("payroll", s.manage(s.handlePayrollDelete)))
	pages.HandleFunc("POST /api/member/pay", s.manage(s.handleMemberPay))
	pages.HandleFunc("GET /schedule", s.module("schedule", s.handleSchedule))
	pages.HandleFunc("POST /api/schedule/cell", s.module("schedule", s.manage(s.handleScheduleCell)))

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

	// Workspace settings, membership and profile.
	pages.HandleFunc("GET /settings/team", s.manage(s.handleTeamSettings))
	pages.HandleFunc("GET /settings/members", s.manage(s.handleTeamMembers))
	pages.HandleFunc("GET /settings/invites", s.manage(s.handleTeamInvites))
	pages.HandleFunc("GET /settings/profile", s.handleSettingsProfile)
	pages.HandleFunc("GET /settings/sections", s.manage(s.handleSectionsPage))
	pages.HandleFunc("GET /settings/preferences", s.handlePreferencesPage)
	pages.HandleFunc("GET /welcome", s.handleWelcome)

	// Public invite-accept page (auth required to actually click Join).
	pages.HandleFunc("GET /invites/{token}", s.handleInviteAcceptPage)

	// Webhook management and audit history.
	pages.HandleFunc("GET /settings/webhooks", s.manage(s.handleWebhooksPage))
	pages.HandleFunc("POST /api/webhooks", s.manage(s.handleWebhookCreate))
	pages.HandleFunc("POST /api/webhooks/{id}/test", s.manage(s.handleWebhookTest))
	pages.HandleFunc("POST /api/webhooks/{id}/delete", s.manage(s.handleWebhookDelete))
	pages.HandleFunc("GET /settings/audit", s.manage(s.handleAuditPage))

	// Page routes are mounted behind pageAuth by routes().
}
