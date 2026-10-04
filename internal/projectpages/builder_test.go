package projectpages

import (
	"context"
	"errors"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

type projectReaderStub struct {
	detail model.ProjectDetail
	err    error
}

func (stub projectReaderStub) Detail(context.Context, appmodel.ProjectDetailRequest) (model.ProjectDetail, error) {
	return stub.detail, stub.err
}

type teamSettingsReaderStub struct {
	currency string
	err      error
}

func (stub teamSettingsReaderStub) Currency(context.Context, int64) (string, error) {
	return stub.currency, stub.err
}

type teamMembershipReaderStub struct {
	role   model.TeamRole
	member bool
	err    error
}

func (stub teamMembershipReaderStub) IsMember(context.Context, int64, int64) (model.TeamRole, bool, error) {
	return stub.role, stub.member, stub.err
}

type invoiceHistoryReaderStub struct {
	teamID, projectID int64
	rows              []model.UnbilledProject
	err               error
}

func (stub *invoiceHistoryReaderStub) UnbilledProjectTime(_ context.Context, teamID, projectID int64) ([]model.UnbilledProject, error) {
	stub.teamID, stub.projectID = teamID, projectID
	return stub.rows, stub.err
}

func TestBuildAssemblesProjectAndOptionalInvoiceHistory(t *testing.T) {
	detail := model.ProjectDetail{Project: model.Project{ID: 18, TeamID: 4, Slug: "alpha"}}
	unbilled := []model.UnbilledProject{{ProjectID: 18, ProjectName: "Alpha"}}
	invoices := &invoiceHistoryReaderStub{rows: unbilled}
	builder, err := New(Dependencies{
		Projects: projectReaderStub{detail: detail}, Teams: teamSettingsReaderStub{currency: "EUR"},
		Memberships: teamMembershipReaderStub{role: model.TeamRoleOwner, member: true}, Invoicing: invoices,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := builder.Build(context.Background(), appmodel.ProjectPageRequest{
		TeamID: 4, CallerID: 12, Slug: "alpha", IncludeUnbilled: true,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if snapshot.Detail.Project.ID != detail.Project.ID || snapshot.TeamCurrency != "EUR" || len(snapshot.Unbilled) != 1 || snapshot.Unbilled[0] != unbilled[0] {
		t.Fatalf("project page snapshot = %+v", snapshot)
	}
	if invoices.teamID != 4 || invoices.projectID != 18 {
		t.Fatalf("invoice history scope = team %d project %d", invoices.teamID, invoices.projectID)
	}
}

func TestBuildSkipsInvoiceHistoryWhenNotRequested(t *testing.T) {
	invoices := &invoiceHistoryReaderStub{}
	builder, err := New(Dependencies{
		Projects: projectReaderStub{detail: model.ProjectDetail{Project: model.Project{ID: 18}}},
		Teams:    teamSettingsReaderStub{currency: "USD"}, Memberships: teamMembershipReaderStub{}, Invoicing: invoices,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Build(context.Background(), appmodel.ProjectPageRequest{
		TeamID: 4, Slug: "alpha",
	}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if invoices.teamID != 0 || invoices.projectID != 0 {
		t.Fatalf("optional invoice history was queried: %+v", invoices)
	}
}

func TestBuildPropagatesWorkspaceCurrencyFailure(t *testing.T) {
	wantErr := errors.New("workspace settings unavailable")
	builder, err := New(Dependencies{
		Projects: projectReaderStub{}, Teams: teamSettingsReaderStub{err: wantErr},
		Memberships: teamMembershipReaderStub{}, Invoicing: &invoiceHistoryReaderStub{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Build(context.Background(), appmodel.ProjectPageRequest{
		TeamID: 4, Slug: "alpha",
	}); !errors.Is(err, wantErr) {
		t.Fatalf("Build error = %v, want wrapped workspace settings error", err)
	}
}

func TestBuildDoesNotReadInvoiceHistoryForNonManager(t *testing.T) {
	invoices := &invoiceHistoryReaderStub{}
	builder, err := New(Dependencies{
		Projects:    projectReaderStub{detail: model.ProjectDetail{Project: model.Project{ID: 18}}},
		Teams:       teamSettingsReaderStub{currency: "USD"},
		Memberships: teamMembershipReaderStub{role: model.TeamRoleMember, member: true},
		Invoicing:   invoices,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := builder.Build(context.Background(), appmodel.ProjectPageRequest{
		TeamID: 4, CallerID: 12, Slug: "alpha", IncludeUnbilled: true,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(snapshot.Unbilled) != 0 || invoices.teamID != 0 || invoices.projectID != 0 {
		t.Fatalf("non-manager received invoice history: snapshot=%+v invoice query=%+v", snapshot, invoices)
	}
}
