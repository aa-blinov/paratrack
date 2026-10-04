package appmodel

import (
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

// IntegrationSummary is the safe read representation of a connected integration.
type IntegrationSummary struct {
	ID        int64
	TeamID    int64
	Provider  string
	Name      string
	CreatedAt time.Time
}

// ExternalTaskWithProvider joins an imported task to its provider name.
type ExternalTaskWithProvider struct {
	ID            int64
	IntegrationID int64
	Title         string
	URL           string
	Status        string
	Provider      string
}

type IntegrationConnectResult struct {
	Integration IntegrationSummary
	Imported    int
	SyncError   error
}

type IntegrationManagementSnapshot struct {
	Items []IntegrationManagementItem
}

type IntegrationManagementItem struct {
	Integration IntegrationSummary
	TaskCount   int
}

// IntegrationDetailSnapshot contains one connection and its imported tasks.
type IntegrationDetailSnapshot struct {
	Integration IntegrationSummary
	Tasks       []model.ExternalTask
}
