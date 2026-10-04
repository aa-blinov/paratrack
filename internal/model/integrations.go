package model

import (
	"time"
)

// IntegrationSummary is the safe list representation of an integration; it
// deliberately contains no credential or provider configuration.
type IntegrationSummary struct {
	ID        int64
	TeamID    int64
	Provider  string
	Name      string
	CreatedAt time.Time
}

// ExternalTask is a task imported from a connected provider.
type ExternalTask struct {
	ID            int64
	IntegrationID int64
	ExternalID    string
	Title         string
	URL           string
	Status        string
	ActivityID    int64
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
