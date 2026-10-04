package appmodel

import "github.com/aa-blinov/paratrack/internal/model"

type IntegrationConnectResult struct {
	Integration model.IntegrationSummary
	Imported    int
	SyncError   error
}

type IntegrationManagementSnapshot struct {
	Items []IntegrationManagementItem
}

type IntegrationManagementItem struct {
	Integration model.IntegrationSummary
	TaskCount   int
}

// IntegrationDetailSnapshot contains one connection and its imported tasks.
type IntegrationDetailSnapshot struct {
	Integration model.IntegrationSummary
	Tasks       []model.ExternalTask
}
