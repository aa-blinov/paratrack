package clients

import (
	"context"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// List returns the client directory for a workspace.
func (s *Service) List(ctx context.Context, query appmodel.ClientCatalogQuery) ([]model.Client, error) {
	if query.TeamID <= 0 {
		return nil, appmodel.ErrInvalidClientTeam
	}
	return s.directory.ListClients(ctx, query)
}

// GetInTeam fetches one client inside its workspace.
func (s *Service) GetInTeam(ctx context.Context, query appmodel.ClientScopeQuery) (model.Client, error) {
	if query.TeamID <= 0 || query.ClientID <= 0 {
		return model.Client{}, appmodel.ErrInvalidClientTeam
	}
	return s.directory.GetClientInTeam(ctx, query)
}

// ForProject resolves the client card behind a project. Callers treat
// model.ErrNotFound as "this project has no card yet" and fall back to the
// recipient snapshot stored on the project itself, which is why a project
// created before this feature keeps working unchanged.
func (s *Service) ForProject(ctx context.Context, query appmodel.ProjectScopeQuery) (model.Client, error) {
	if query.TeamID <= 0 || query.ProjectID <= 0 {
		return model.Client{}, appmodel.ErrInvalidClientTeam
	}
	return s.directory.GetClientForProject(ctx, query)
}

// Create mints a client card.
func (s *Service) Create(ctx context.Context, request appmodel.ClientCreateRequest) (model.Client, error) {
	if request.TeamID <= 0 {
		return model.Client{}, appmodel.ErrInvalidClientTeam
	}
	return s.writes.CreateClient(ctx, request)
}

// Update applies the editable client fields.
func (s *Service) Update(ctx context.Context, request appmodel.ClientUpdateRequest) (model.Client, error) {
	if request.TeamID <= 0 || request.ClientID <= 0 {
		return model.Client{}, appmodel.ErrInvalidClientTeam
	}
	return s.writes.UpdateClient(ctx, request)
}

// Archive hides a client from pickers without touching the invoices it was
// addressed to. Deleting would orphan exactly the history that makes the card
// useful, so the archive flag is the only removal this service offers.
func (s *Service) Archive(ctx context.Context, request appmodel.ClientUpdateRequest) (model.Client, error) {
	if request.TeamID <= 0 || request.ClientID <= 0 {
		return model.Client{}, appmodel.ErrInvalidClientTeam
	}
	archived := true
	update := request.Update
	update.Archived = &archived
	request.Update = update
	return s.writes.UpdateClient(ctx, request)
}

// Restore brings an archived client back into the pickers.
func (s *Service) Restore(ctx context.Context, request appmodel.ClientUpdateRequest) (model.Client, error) {
	if request.TeamID <= 0 || request.ClientID <= 0 {
		return model.Client{}, appmodel.ErrInvalidClientTeam
	}
	archived := false
	update := request.Update
	update.Archived = &archived
	request.Update = update
	return s.writes.UpdateClient(ctx, request)
}

// Budgets prices every project in the workspace against its ceiling. The
// project and client screens both need it, and both must read it from one
// place: two implementations would be two chances for the number on screen to
// disagree with the invoice.
func (s *Service) Budgets(ctx context.Context, query appmodel.ProjectBudgetQuery) (map[int64]appmodel.ProjectBudget, error) {
	if query.TeamID <= 0 {
		return nil, appmodel.ErrInvalidClientTeam
	}
	return s.budgets.ProjectBudgetSummaries(ctx, query)
}

// Projects lists the work attached to a client card.
func (s *Service) Projects(ctx context.Context, query appmodel.ClientScopeQuery) ([]appmodel.ClientProject, error) {
	if query.TeamID <= 0 || query.ClientID <= 0 {
		return nil, appmodel.ErrInvalidClientTeam
	}
	return s.directory.ClientProjects(ctx, query)
}

// Invoices lists the documents addressed to a client. clientName is the
// card's own name and is needed for the legacy match: invoices written
// before cards existed name their recipient and nothing else.
func (s *Service) Invoices(ctx context.Context, query appmodel.ClientScopeQuery, clientName string) ([]appmodel.ClientInvoice, error) {
	if query.TeamID <= 0 || query.ClientID <= 0 {
		return nil, appmodel.ErrInvalidClientTeam
	}
	return s.directory.ClientInvoices(ctx, query, clientName)
}
