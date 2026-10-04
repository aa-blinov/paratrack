package projectpages

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/sessiondecorations"
)

type projectReaderStub struct {
	detail     model.ProjectDetail
	summaries  map[int64]model.ProjectSummary
	summaryIDs []int64
	err        error
	summaryErr error
}

func (stub *projectReaderStub) Detail(context.Context, appmodel.ProjectDetailRequest) (model.ProjectDetail, error) {
	return stub.detail, stub.err
}

func (stub *projectReaderStub) Summaries(_ context.Context, query appmodel.ProjectSummariesQuery) (map[int64]model.ProjectSummary, error) {
	stub.summaryIDs = append([]int64(nil), query.ProjectIDs...)
	return stub.summaries, stub.summaryErr
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

type sessionTagReaderStub struct {
	bySession map[int64][]model.Tag
	ids       []int64
	err       error
}

type sessionReaderStub struct{}

func (sessionReaderStub) SessionActivity(context.Context, int64, int64) (model.Session, model.Activity, error) {
	return model.Session{}, model.Activity{}, nil
}

func (stub *sessionTagReaderStub) TagsForSessions(_ context.Context, _ int64, ids []int64) (map[int64][]model.Tag, error) {
	stub.ids = append([]int64(nil), ids...)
	return stub.bySession, stub.err
}

type loggerStub struct{ messages []string }

func (stub *loggerStub) Printf(format string, args ...any) {
	stub.messages = append(stub.messages, fmt.Sprintf(format, args...))
}

func decorationsForTest(t *testing.T, projects *projectReaderStub, tags *sessionTagReaderStub, logger *loggerStub) SessionDecorationBuilder {
	t.Helper()
	builder, err := sessiondecorations.New(sessiondecorations.Dependencies{Sessions: sessionReaderStub{}, Tags: tags, Projects: projects, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	return builder
}

func (stub *invoiceHistoryReaderStub) UnbilledProjectTime(_ context.Context, teamID, projectID int64) ([]model.UnbilledProject, error) {
	stub.teamID, stub.projectID = teamID, projectID
	return stub.rows, stub.err
}

func TestBuildAssemblesProjectAndOptionalInvoiceHistory(t *testing.T) {
	detail := model.ProjectDetail{
		Project: model.Project{ID: 18, TeamID: 4, Slug: "alpha"},
		Activity: model.ProjectActivitySummary{Recent: []model.ActiveSession{{
			Session: model.Session{ID: 31}, Activity: model.Activity{ProjectID: 12},
		}}},
	}
	unbilled := []model.UnbilledProject{{ProjectID: 18, ProjectName: "Alpha"}}
	invoices := &invoiceHistoryReaderStub{rows: unbilled}
	projects := &projectReaderStub{
		detail: detail, summaries: map[int64]model.ProjectSummary{12: {ID: 12, Name: "Project"}},
	}
	tags := &sessionTagReaderStub{bySession: map[int64][]model.Tag{31: {{ID: 7, Name: "urgent"}}}}
	builder, err := New(Dependencies{
		Projects: projects, Teams: teamSettingsReaderStub{currency: "EUR"},
		Memberships: teamMembershipReaderStub{role: model.TeamRoleOwner, member: true}, Invoicing: invoices,
		Decorations: decorationsForTest(t, projects, tags, &loggerStub{}),
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
	if len(tags.ids) != 1 || tags.ids[0] != 31 || len(snapshot.TagsBySession[31]) != 1 || snapshot.ProjectsByID[12].Name != "Project" {
		t.Fatalf("session decorations were not assembled: %+v", snapshot)
	}
	if len(projects.summaryIDs) != 1 || projects.summaryIDs[0] != 12 {
		t.Fatalf("project summary IDs = %v", projects.summaryIDs)
	}
	if invoices.teamID != 4 || invoices.projectID != 18 {
		t.Fatalf("invoice history scope = team %d project %d", invoices.teamID, invoices.projectID)
	}
}

func TestBuildSkipsInvoiceHistoryWhenNotRequested(t *testing.T) {
	invoices := &invoiceHistoryReaderStub{}
	builder, err := New(Dependencies{
		Projects: &projectReaderStub{detail: model.ProjectDetail{Project: model.Project{ID: 18}}},
		Teams:    teamSettingsReaderStub{currency: "USD"}, Memberships: teamMembershipReaderStub{}, Invoicing: invoices,
		Decorations: decorationsForTest(t, &projectReaderStub{}, &sessionTagReaderStub{}, &loggerStub{}),
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
		Projects: &projectReaderStub{}, Teams: teamSettingsReaderStub{err: wantErr},
		Memberships: teamMembershipReaderStub{}, Invoicing: &invoiceHistoryReaderStub{},
		Decorations: decorationsForTest(t, &projectReaderStub{}, &sessionTagReaderStub{}, &loggerStub{}),
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
		Projects:    &projectReaderStub{detail: model.ProjectDetail{Project: model.Project{ID: 18}}},
		Teams:       teamSettingsReaderStub{currency: "USD"},
		Memberships: teamMembershipReaderStub{role: model.TeamRoleMember, member: true},
		Invoicing:   invoices,
		Decorations: decorationsForTest(t, &projectReaderStub{}, &sessionTagReaderStub{}, &loggerStub{}),
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

func TestBuildKeepsDetailWhenSessionDecorationReadsFail(t *testing.T) {
	projects := &projectReaderStub{
		detail: model.ProjectDetail{
			Project: model.Project{ID: 18},
			Activity: model.ProjectActivitySummary{Recent: []model.ActiveSession{{
				Session: model.Session{ID: 31}, Activity: model.Activity{ProjectID: 12},
			}}},
		},
		summaryErr: errors.New("project summaries unavailable"),
	}
	tags := &sessionTagReaderStub{err: errors.New("session tags unavailable")}
	logger := &loggerStub{}
	builder, err := New(Dependencies{
		Projects: projects, Teams: teamSettingsReaderStub{currency: "USD"},
		Memberships: teamMembershipReaderStub{}, Invoicing: &invoiceHistoryReaderStub{},
		Decorations: decorationsForTest(t, projects, tags, logger),
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := builder.Build(context.Background(), appmodel.ProjectPageRequest{
		TeamID: 4, Slug: "alpha",
	})
	if err != nil {
		t.Fatalf("Build should retain project detail: %v", err)
	}
	if snapshot.Detail.Project.ID != 18 || len(logger.messages) != 2 {
		t.Fatalf("decoration failures: project=%+v logs=%v", snapshot.Detail.Project, logger.messages)
	}
}
