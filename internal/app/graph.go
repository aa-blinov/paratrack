package app

import (
	"io"

	"github.com/aa-blinov/paratrack/internal/audit"
	"github.com/aa-blinov/paratrack/internal/auth"
	"github.com/aa-blinov/paratrack/internal/billing"
	"github.com/aa-blinov/paratrack/internal/dashboard"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/goals"
	"github.com/aa-blinov/paratrack/internal/importing"
	"github.com/aa-blinov/paratrack/internal/integrations"
	"github.com/aa-blinov/paratrack/internal/invoicedocuments"
	"github.com/aa-blinov/paratrack/internal/invoicing"
	"github.com/aa-blinov/paratrack/internal/mailqueue"
	"github.com/aa-blinov/paratrack/internal/memberadmin"
	"github.com/aa-blinov/paratrack/internal/payroll"
	"github.com/aa-blinov/paratrack/internal/payrollops"
	"github.com/aa-blinov/paratrack/internal/preferences"
	"github.com/aa-blinov/paratrack/internal/projectpages"
	"github.com/aa-blinov/paratrack/internal/projects"
	"github.com/aa-blinov/paratrack/internal/push"
	savedreports "github.com/aa-blinov/paratrack/internal/reports"
	"github.com/aa-blinov/paratrack/internal/scheduling"
	"github.com/aa-blinov/paratrack/internal/sessiondecorations"
	"github.com/aa-blinov/paratrack/internal/tagging"
	"github.com/aa-blinov/paratrack/internal/teamops"
	"github.com/aa-blinov/paratrack/internal/teams"
	"github.com/aa-blinov/paratrack/internal/tracking"
	"github.com/aa-blinov/paratrack/internal/trackingops"
	"github.com/aa-blinov/paratrack/internal/webhooks"
)

// Services holds concrete workflow implementations assembled from the
// persistence and outbound adapters. Transport packages receive only their
// own consumer-defined interfaces.
type Services struct {
	Billing            *billing.Service
	Auth               *auth.Service
	AuditLog           *audit.Service
	Teams              *teams.Service
	TeamOps            *teamops.Service
	Tracking           *tracking.Service
	TrackingOps        *trackingops.Service
	Imports            *importing.Service
	Integrations       *integrations.Service
	Invoicing          *invoicing.Service
	InvoiceDocuments   *invoicedocuments.Builder
	Payroll            *payroll.Service
	PayrollPaid        *payrollops.Service
	Preferences        *preferences.Service
	Scheduling         *scheduling.Service
	Projects           *projects.Service
	ProjectPages       *projectpages.Builder
	Reports            *savedreports.Service
	ReportBuilder      *savedreports.Builder
	Dashboard          *dashboard.Builder
	SessionDecorations *sessiondecorations.Builder
	MemberAdmin        *memberadmin.Service
	Push               *push.Service
	Tagging            *tagging.Service
	Goals              *goals.Service
	Webhooks           *webhooks.Service
	MailQueue          *mailqueue.Service
	Resources          io.Closer
}

// Close stops application workers and releases outbound clients owned by this graph.
func (s *Services) Close() error {
	if s == nil || depcheck.IsNil(s.Resources) {
		return nil
	}
	return s.Resources.Close()
}
