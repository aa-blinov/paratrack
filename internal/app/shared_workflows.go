package app

import (
	"fmt"

	"github.com/aa-blinov/paratrack/internal/audit"
	"github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/goals"
	"github.com/aa-blinov/paratrack/internal/projects"
	"github.com/aa-blinov/paratrack/internal/tagging"
	"github.com/aa-blinov/paratrack/internal/tracking"
)

// sharedWorkflows contains workflows assembled by both the server and CLI
// graphs. Keep their persistence wiring in one place so the adapters expose
// the same project, tracking, tagging, goal, and audit behavior.
type sharedWorkflows struct {
	Projects *projects.Service
	Tracking *tracking.Service
	Tagging  *tagging.Service
	Goals    *goals.Service
	Audit    *audit.Service
}

func newSharedWorkflows(database *db.DB) (sharedWorkflows, error) {
	projectService, err := projects.NewService(projects.Dependencies{
		Catalog: database, Usage: database,
		Billing: database, Authorization: database, Writes: database,
	})
	if err != nil {
		return sharedWorkflows{}, fmt.Errorf("construct projects service: %w", err)
	}

	auditService, err := audit.New(database)
	if err != nil {
		return sharedWorkflows{}, fmt.Errorf("construct audit service: %w", err)
	}

	trackingService, err := tracking.New(tracking.Dependencies{
		Sessions: database, Queries: database,
		Activities: database, Timesheets: database,
	})
	if err != nil {
		return sharedWorkflows{}, fmt.Errorf("construct tracking service: %w", err)
	}

	taggingService, err := tagging.New(tagging.Dependencies{
		Tags: database, SessionActivities: database,
	})
	if err != nil {
		return sharedWorkflows{}, fmt.Errorf("construct tagging service: %w", err)
	}

	goalService, err := goals.New(goals.Dependencies{
		Activities: database, Goals: database, Writes: database,
	})
	if err != nil {
		return sharedWorkflows{}, fmt.Errorf("construct goals service: %w", err)
	}

	return sharedWorkflows{
		Projects: projectService,
		Tracking: trackingService,
		Tagging:  taggingService,
		Goals:    goalService,
		Audit:    auditService,
	}, nil
}
