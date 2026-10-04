// Command paratrack starts the command-line application.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aa-blinov/paratrack/internal/app"
	"github.com/aa-blinov/paratrack/internal/cli"
	"github.com/aa-blinov/paratrack/internal/cliport"
	"github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/mail"
	"github.com/aa-blinov/paratrack/internal/netclients"
	"github.com/aa-blinov/paratrack/internal/oidcclient"
	"github.com/aa-blinov/paratrack/internal/web"
)

func main() {
	location, err := loadCLITimezone()
	if err != nil {
		fmt.Fprintf(os.Stderr, "paratrack: configuration: %v\n", err)
		os.Exit(2)
	}
	runtime := cli.NewRuntime(os.Stdin, os.Stdout, os.Stderr)
	logger := log.Default()
	now := func() time.Time { return time.Now().In(location) }
	runtime.Now = now
	cliServiceConfig := loadCLIServiceConfig(logger)
	processContext, cancelContext := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	runtime.Context = processContext
	defer cancelContext()
	runtime.ServiceLoader = func(ctx context.Context) (*cliport.Services, io.Closer, error) {
		databaseConfig := loadDatabaseConfig(logger, now)
		database, err := db.OpenConfiguredContext(ctx, databaseConfig)
		if err != nil {
			return nil, nil, fmt.Errorf("open database: %w", err)
		}
		services, applicationResources, err := app.NewCLIServices(database, cliServiceConfig)
		if err != nil {
			return nil, nil, errors.Join(fmt.Errorf("compose application: %w", err), database.Close())
		}
		return services, serviceResources{database: database, application: applicationResources}, nil
	}
	runtime.WebRunner = func(ctx context.Context, addr string) (runErr error) {
		config, err := loadProcessConfigWithTimezone(os.Getenv, location)
		if err != nil {
			return &cli.ExitError{Code: 2, Err: fmt.Errorf("configuration: %w", err)}
		}
		database, err := db.OpenConfiguredContext(ctx, config.database)
		if err != nil {
			return err
		}
		defer func() { runErr = errors.Join(runErr, database.Close()) }()

		services, err := app.NewServices(database, config.services)
		if err != nil {
			return fmt.Errorf("compose application: %w", err)
		}
		defer func() { runErr = errors.Join(runErr, services.Close()) }()
		externalHTTPClient := netclients.External()
		runtimeDeps := web.RuntimeDependencies{}
		defer runtimeDeps.Close()
		var oidcProvider web.OIDCProvider
		if config.web.OIDCEnabled {
			oidcProvider, err = oidcclient.New(externalHTTPClient, config.oidc)
			if err != nil {
				externalHTTPClient.CloseIdleConnections()
				return fmt.Errorf("init OIDC client: %w", err)
			}
			runtimeDeps.OIDC = oidcProvider
		} else {
			externalHTTPClient.CloseIdleConnections()
		}
		mailer, err := mail.NewSender(config.mailer, config.services.Logger)
		if err != nil {
			return fmt.Errorf("construct mail sender: %w", err)
		}
		runtimeDeps.Mailer = mailer
		runtimeDeps.MailQueueWorker = services.MailQueue
		runtimeDeps.WebhookDeliveryWorker = services.Webhooks
		server, err := web.New(web.Dependencies{
			Billing: services.Billing,
			Auth: web.AuthenticationDependencies{
				Identity: services.Auth, SignIn: services.Auth, Recovery: services.Auth,
				Profile: services.Auth, APITokens: services.Auth,
			},
			TokenAdmin: services.TokenAdmin,
			AuditLog:   services.AuditLog,
			Teams: web.TeamDependencies{
				Directory: services.Teams, Invitations: services.Teams,
				Settings: services.Teams, Administration: services.Teams,
			},
			TeamOps: services.TeamOps,
			Tracking: web.TrackingDependencies{
				Queries: services.Tracking, Commands: services.Tracking,
			},
			TrackingOps: services.TrackingOps,
			Imports:     services.Imports,
			Integrations: web.IntegrationDependencies{
				Queries: services.Integrations, Commands: services.Integrations,
			},
			Invoicing: web.InvoiceDependencies{
				Queries: services.Invoicing, Drafts: services.Invoicing,
				PaymentLinks: services.Invoicing,
			},
			InvoiceDocuments:   services.InvoiceDocuments,
			Payroll:            services.Payroll,
			PayrollPaid:        services.PayrollPaid,
			Preferences:        services.Preferences,
			Scheduling:         services.Scheduling,
			Projects:           web.ProjectDependencies{Queries: services.Projects, Commands: services.Projects},
			ProjectPages:       services.ProjectPages,
			Reports:            services.Reports,
			ReportBuilder:      services.ReportBuilder,
			Dashboard:          services.Dashboard,
			SessionDecorations: services.SessionDecorations,
			MemberAdmin:        services.MemberAdmin,
			Push:               services.Push,
			Tagging: web.TagDependencies{
				Queries: services.Tagging, Commands: services.Tagging,
			},
			Goals:     services.Goals,
			Webhooks:  services.Webhooks,
			MailQueue: services.MailQueue,
		}, addr, config.web, runtimeDeps)
		if err != nil {
			return fmt.Errorf("init web server: %w", err)
		}
		if err := server.ListenAndServeContext(ctx); err != nil {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	}
	err = cli.RunWithRuntime(os.Args[1:], runtime)
	err = errors.Join(err, closeRuntime(runtime))
	if err != nil {
		code := 1
		var exitErr *cli.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.Code
		}
		if err.Error() != "" {
			fmt.Fprintf(os.Stderr, "paratrack: %v\n", err)
		}
		os.Exit(code)
	}
}

func closeRuntime(runtime *cli.Runtime) error {
	if err := runtime.Close(); err != nil {
		return fmt.Errorf("close application: %w", err)
	}
	return nil
}

type serviceResources struct {
	database    *db.DB
	application io.Closer
}

func (r serviceResources) Close() error {
	var applicationErr error
	if r.application != nil {
		applicationErr = r.application.Close()
	}
	if r.database != nil {
		return errors.Join(applicationErr, r.database.Close())
	}
	return applicationErr
}
