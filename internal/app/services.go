// Package app assembles application services around infrastructure adapters.
// Entrypoints create this dependency graph and pass it to their transports.
package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aa-blinov/paratrack/internal/auth"
	"github.com/aa-blinov/paratrack/internal/billing"
	"github.com/aa-blinov/paratrack/internal/dashboard"
	"github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/importing"
	"github.com/aa-blinov/paratrack/internal/importproviders"
	"github.com/aa-blinov/paratrack/internal/integrations"
	"github.com/aa-blinov/paratrack/internal/integrations/providers"
	"github.com/aa-blinov/paratrack/internal/invoicedocuments"
	"github.com/aa-blinov/paratrack/internal/invoicing"
	"github.com/aa-blinov/paratrack/internal/mailqueue"
	"github.com/aa-blinov/paratrack/internal/memberadmin"
	"github.com/aa-blinov/paratrack/internal/netclients"
	"github.com/aa-blinov/paratrack/internal/payroll"
	"github.com/aa-blinov/paratrack/internal/payrollops"
	"github.com/aa-blinov/paratrack/internal/preferences"
	"github.com/aa-blinov/paratrack/internal/projectpages"
	savedreports "github.com/aa-blinov/paratrack/internal/reports"
	"github.com/aa-blinov/paratrack/internal/scheduling"
	"github.com/aa-blinov/paratrack/internal/sessiondecorations"
	"github.com/aa-blinov/paratrack/internal/teamops"
	"github.com/aa-blinov/paratrack/internal/teams"
	"github.com/aa-blinov/paratrack/internal/trackingops"
)

var ErrNilDatabase = errors.New("application database is nil")

type applicationResources struct {
	webhooks  workerShutdown
	push      workerShutdown
	clients   idleHTTPClients
	closeOnce sync.Once
	closeErr  error
}

type workerShutdown interface {
	Shutdown(context.Context) error
}

func (r *applicationResources) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdownResults := make(chan error, 2)
		shutdownCount := 0
		if r.webhooks != nil {
			shutdownCount++
			go func() { shutdownResults <- r.webhooks.Shutdown(ctx) }()
		}
		if r.push != nil {
			shutdownCount++
			go func() { shutdownResults <- r.push.Shutdown(ctx) }()
		}
		for range shutdownCount {
			r.closeErr = errors.Join(r.closeErr, <-shutdownResults)
		}
		r.closeErr = errors.Join(r.closeErr, r.clients.Close())
	})
	return r.closeErr
}

// NewServices constructs concrete server workflows from infrastructure
// adapters. The process root maps them to each transport's consumer ports.
// It requires an initialized database adapter.
func NewServices(database *db.DB, config Config) (result *Services, returnErr error) {
	if database == nil {
		return nil, ErrNilDatabase
	}
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	logger := config.Logger
	resources := &applicationResources{}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, resources.Close())
		}
	}()
	now := config.Now
	shared, err := newSharedWorkflows(database)
	if err != nil {
		return nil, err
	}
	projectService := shared.Projects
	trackingService := shared.Tracking
	taggingService := shared.Tagging
	sessionDecorationBuilder, err := sessiondecorations.New(sessiondecorations.Dependencies{
		Tags: taggingService, Projects: projectService, Logger: logger,
	})
	if err != nil {
		return nil, fmt.Errorf("construct session decoration builder: %w", err)
	}
	goalService := shared.Goals
	auditService := shared.Audit
	authService, err := auth.NewService(auth.Dependencies{
		Users: database, Sessions: database, Resets: database,
		Tokens: database, Memberships: database, Now: now, Audit: auditService, Logger: logger,
	})
	if err != nil {
		return nil, fmt.Errorf("construct auth service: %w", err)
	}
	teamService, err := teams.NewService(teams.Dependencies{
		Teams: database, Memberships: database, Invites: database,
		Settings: database, Modules: database, Now: now,
	})
	if err != nil {
		return nil, fmt.Errorf("construct teams service: %w", err)
	}
	stripeHTTPClient := netclients.External()
	resources.clients = append(resources.clients, stripeHTTPClient)
	webhookService, pushService, err := newDeliveryServices(database, database, config.WebhookAllowPrivate, logger, now, auditService, resources)
	if err != nil {
		return nil, err
	}
	invoiceService, err := invoicing.NewService(invoicing.Dependencies{
		Reader: database, Projects: database, Writer: database, Authorizer: database, StripeCredentials: database,
		StripeGateway: stripeProtocol{client: stripeHTTPClient},
		StripeAPIKey:  config.StripeAPIKey, StripeWebhookSecret: config.StripeWebhookSecret,
		Audit: auditService, Logger: logger,
	})
	if err != nil {
		return nil, fmt.Errorf("construct invoicing service: %w", err)
	}
	invoiceDocuments, err := invoicedocuments.New(invoicedocuments.Dependencies{
		Invoices: invoiceService, Teams: teamService, Logger: logger,
	})
	if err != nil {
		return nil, fmt.Errorf("construct invoice document builder: %w", err)
	}
	projectPageBuilder, err := projectpages.New(projectpages.Dependencies{
		Projects: projectService, Teams: teamService, Memberships: teamService, Invoicing: invoiceService,
		Decorations: sessionDecorationBuilder,
	})
	if err != nil {
		return nil, fmt.Errorf("construct project page builder: %w", err)
	}
	reportService, err := savedreports.New(database)
	if err != nil {
		return nil, fmt.Errorf("construct saved reports service: %w", err)
	}
	teamOpsService, err := teamops.New(teamops.Dependencies{
		Mutations: teamService, Workspace: workspaceLifecycle{teams: teamService},
		Sessions: authService, Audit: auditService, Logger: logger,
	})
	if err != nil {
		return nil, fmt.Errorf("construct team operations: %w", err)
	}
	payrollService, err := payroll.NewServiceWithClock(database, now, auditService, logger)
	if err != nil {
		return nil, fmt.Errorf("construct payroll service: %w", err)
	}
	memberAdminService, err := memberadmin.New(memberadmin.Dependencies{Members: teamService, Payroll: payrollService})
	if err != nil {
		return nil, fmt.Errorf("construct member administration service: %w", err)
	}
	schedulingService, err := scheduling.New(scheduling.Dependencies{Store: database, Projects: projectService})
	if err != nil {
		return nil, fmt.Errorf("construct scheduling service: %w", err)
	}
	mailQueueService, err := mailqueue.New(database, now, logger, auditService)
	if err != nil {
		return nil, fmt.Errorf("construct mail queue service: %w", err)
	}
	providerHTTPClient := netclients.Providers(config.IntegrationAllowPrivate)
	resources.clients = append(resources.clients, providerHTTPClient)
	payrollPaidService, err := payrollops.New(payrollops.Dependencies{
		Payments: payrollService, Audit: auditService,
		Notifications: payrollPushNotifications{push: pushService}, Logger: logger,
	})
	if err != nil {
		return nil, fmt.Errorf("construct payroll payment operations: %w", err)
	}
	importProviderService, err := importproviders.New(providerHTTPClient, config.DefaultTimezone, now)
	if err != nil {
		return nil, fmt.Errorf("construct import providers: %w", err)
	}
	importService, err := importing.New(database, importProviderService, database, auditService, logger)
	if err != nil {
		return nil, fmt.Errorf("construct importing service: %w", err)
	}
	providerClient, err := providers.NewConfigured(providerHTTPClient, providers.Config{
		JiraSite:   config.JiraSite,
		GitLabSite: config.GitLabSite,
	})
	if err != nil {
		return nil, fmt.Errorf("construct integration provider client: %w", err)
	}
	integrationService, err := integrations.New(integrations.Dependencies{
		Manager: database, Catalog: database, Tasks: database, SyncStarter: database,
		Audit: auditService, Logger: logger,
	}, providerClient)
	if err != nil {
		return nil, fmt.Errorf("construct integrations service: %w", err)
	}
	reportBuilder, err := savedreports.NewBuilder(savedreports.BuilderDependencies{
		Sessions: trackingService, Teams: teamService,
		Projects: projectService, Users: authService, Tags: taggingService, Decorations: sessionDecorationBuilder,
	})
	if err != nil {
		return nil, fmt.Errorf("construct report builder: %w", err)
	}
	preferenceService, err := preferences.New(database, projectService)
	if err != nil {
		return nil, fmt.Errorf("construct preferences service: %w", err)
	}
	trackingOpsService, err := trackingops.New(trackingops.Dependencies{
		Sessions: trackingService, Activities: trackingService, Resolver: trackingService,
		Projects: projectService, ClosedSessions: trackingService, Goals: goalService,
		Audit:         auditService,
		Notifications: trackingPushNotifications{push: pushService}, Logger: logger,
	})
	if err != nil {
		return nil, fmt.Errorf("construct tracking operations: %w", err)
	}
	billingService, err := billing.New(billing.Dependencies{
		Payments: stripeInvoiceProcessor{service: invoiceService}, ManualPayments: invoiceService,
		Audit: auditService, Logger: logger,
	})
	if err != nil {
		return nil, fmt.Errorf("construct billing service: %w", err)
	}
	dashboardBuilder, err := dashboard.NewBuilder(dashboard.Dependencies{
		Goals: goalService, Tracking: trackingService,
		Projects: projectService, Decorations: sessionDecorationBuilder, Invoices: invoiceService, Logger: logger,
	})
	if err != nil {
		return nil, fmt.Errorf("construct dashboard builder: %w", err)
	}
	return &Services{
		Billing:            billingService,
		Auth:               authService,
		AuditLog:           auditService,
		Teams:              teamService,
		TeamOps:            teamOpsService,
		Tracking:           trackingService,
		TrackingOps:        trackingOpsService,
		Imports:            importService,
		Integrations:       integrationService,
		Invoicing:          invoiceService,
		InvoiceDocuments:   invoiceDocuments,
		Payroll:            payrollService,
		PayrollPaid:        payrollPaidService,
		Preferences:        preferenceService,
		Scheduling:         schedulingService,
		Projects:           projectService,
		ProjectPages:       projectPageBuilder,
		Reports:            reportService,
		ReportBuilder:      reportBuilder,
		Dashboard:          dashboardBuilder,
		SessionDecorations: sessionDecorationBuilder,
		MemberAdmin:        memberAdminService,
		Push:               pushService,
		Tagging:            taggingService,
		Goals:              goalService,
		Webhooks:           webhookService,
		MailQueue:          mailQueueService,
		Resources:          resources,
	}, nil
}
