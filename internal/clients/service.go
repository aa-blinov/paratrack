// Package clients owns client workflows shared by HTTP and other adapters.
package clients

import (
	"context"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/depcheck"
	"github.com/aa-blinov/paratrack/internal/model"
)

// ClientDirectoryStore reads the client directory and its per-client summary.
type ClientDirectoryStore interface {
	ListClients(context.Context, appmodel.ClientCatalogQuery) ([]model.Client, error)
	GetClientInTeam(context.Context, appmodel.ClientScopeQuery) (model.Client, error)
	GetClientForProject(context.Context, appmodel.ProjectScopeQuery) (model.Client, error)
}

// ClientWriteStore provides atomic client mutations. Authorization is
// re-checked transactionally inside each implementation.
type ClientWriteStore interface {
	CreateClient(context.Context, appmodel.ClientCreateRequest) (model.Client, error)
	UpdateClient(context.Context, appmodel.ClientUpdateRequest) (model.Client, error)
}

// ClientBudgetStore prices what projects have consumed. It lives behind the
// same service so HTTP never reaches into the database for money.
type ClientBudgetStore interface {
	ProjectBudgetSummaries(context.Context, appmodel.ProjectBudgetQuery) (map[int64]appmodel.ProjectBudget, error)
}

// Dependencies keeps reads and writes on independently substitutable ports.
type Dependencies struct {
	Directory ClientDirectoryStore
	Writes    ClientWriteStore
	Budgets   ClientBudgetStore
}

var ErrIncompleteDependencies = errors.New("client service dependencies are incomplete")

type Service struct {
	directory ClientDirectoryStore
	writes    ClientWriteStore
	budgets   ClientBudgetStore
}

func NewService(deps Dependencies) (*Service, error) {
	missing := []struct {
		name string
		port any
	}{
		{"directory", deps.Directory}, {"writes", deps.Writes}, {"budgets", deps.Budgets},
	}
	for _, dependency := range missing {
		if depcheck.IsNil(dependency.port) {
			return nil, fmt.Errorf("%w: %s", ErrIncompleteDependencies, dependency.name)
		}
	}
	return &Service{directory: deps.Directory, writes: deps.Writes, budgets: deps.Budgets}, nil
}
