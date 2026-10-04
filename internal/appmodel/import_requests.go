package appmodel

import "github.com/aa-blinov/paratrack/internal/importport"

// ProviderImportRunRequest carries actor scope and credentials for one provider import.
type ProviderImportRunRequest struct {
	TeamID   int64
	CallerID int64
	Provider importport.ProviderRequest
}

// ImportBatchRequest carries validated import entries and their workspace,
// actor and optional provider provenance into one atomic persistence operation.
type ImportBatchRequest struct {
	TeamID   int64
	CallerID int64
	Provider string
	Entries  []importport.ImportedEntry
}
