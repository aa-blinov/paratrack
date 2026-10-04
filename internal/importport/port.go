// Package importport defines transport-neutral contracts shared by the import
// workflow, provider adapters, and transport consumers.
package importport

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidInput    = errors.New("invalid import provider input")
	ErrInvalidEntry    = errors.New("invalid imported entry")
	ErrApplyFailed     = errors.New("could not apply imported entries")
	ErrEntryLimit      = errors.New("import provider entry limit reached")
	ErrPaginationLimit = errors.New("import provider pagination limit reached")
)

// ProviderRequest contains the credential and range required to fetch entries
// from an external time tracker. Keeping the values named avoids positional
// ambiguity across HTTP, workflow, and provider adapter boundaries.
type ProviderRequest struct {
	Provider string
	Secret   string `json:"-"`
	Extra    string
	From     string
	To       string
	Timezone string
}

// MaxEntries bounds provider memory use, transactional batch size, and outbox
// payload size for one import operation.
const MaxEntries = 20_000

// ProviderFetcher owns provider authentication, HTTP and response parsing.
type ProviderFetcher interface {
	Fetch(context.Context, ProviderRequest) ([]ImportedEntry, error)
}

// ImportedEntry is a validated closed session received from an external time tracker.
type ImportedEntry struct {
	ExternalID string
	Activity   string
	Start      time.Time
	End        time.Time
	Note       string
}
