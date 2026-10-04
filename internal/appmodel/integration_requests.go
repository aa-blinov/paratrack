package appmodel

import "github.com/aa-blinov/paratrack/internal/integrationport"

// IntegrationConfig is the typed provider-specific settings carried by an
// integration command and sync workflow.
type IntegrationConfig = integrationport.ProviderConfig

// IntegrationLookupQuery resolves one integration within its workspace.
type IntegrationLookupQuery struct {
	TeamID        int64
	IntegrationID int64
}

// ExternalTaskLookupQuery resolves one imported task within its workspace.
type ExternalTaskLookupQuery struct {
	TeamID int64
	TaskID int64
}

type IntegrationMutationRequest struct {
	TeamID        int64
	IntegrationID int64
	CallerID      int64
}

type IntegrationSyncStartRequest struct {
	TeamID        int64
	IntegrationID int64
	CallerID      int64
}

// IntegrationSyncCredentials is returned only after an authorized sync start.
// Its fields must never cross a transport boundary through accidental JSON
// serialization.
type IntegrationSyncCredentials struct {
	ID         int64             `json:"-"`
	TeamID     int64             `json:"-"`
	Provider   string            `json:"-"`
	Secret     string            `json:"-"`
	Config     IntegrationConfig `json:"-"`
	Generation int64             `json:"-"`
}

type IntegrationTaskSyncRequest struct {
	TeamID        int64
	IntegrationID int64
	CallerID      int64
	Generation    int64
	Tasks         []integrationport.ProviderTask
}
