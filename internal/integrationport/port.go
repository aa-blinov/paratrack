// Package integrationport defines the provider boundary shared by integration
// orchestration and its outbound adapters.
package integrationport

import (
	"context"
	"errors"
)

// ErrTokenRejected means the provider refused the token itself (401), so no connection with
// it can work. Any other answer, or none, leaves the decision to the first sync (ADM-10).
var ErrTokenRejected = errors.New("the provider rejected the token")

const MaxSyncTasks = 2000

// ProviderConfig contains normalized settings shared by integration
// orchestration and provider adapters. Persistence stores this as JSON.
type ProviderConfig struct {
	Target string
}

// ProviderInput contains credentials and configuration for one scoped fetch.
type ProviderInput struct {
	Provider string
	Secret   string         `json:"-"`
	Config   ProviderConfig `json:"-"`
}

// ProviderClient fetches a complete provider snapshot without exposing
// provider HTTP details to the synchronization workflow.
type ProviderClient interface {
	FetchTasks(context.Context, ProviderInput) ([]ProviderTask, error)
	// CheckToken asks the provider whether it accepts the token, before the connection is saved.
	CheckToken(context.Context, ProviderInput) error
}

// ProviderTask is the normalized provider snapshot item passed to the sync workflow.
type ProviderTask struct {
	ExternalID string
	Title      string
	URL        string
	Status     string
}
