package crm

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// fakeStore records the deal create requests that reach the persistence layer and
// serves the deal list it is given.
type fakeStore struct {
	Store
	created      []appmodel.CRMDealCreateRequest
	updated      []appmodel.CRMDealUpdateRequest
	deals        []model.CRMDeal
	stages       []model.CRMStage
	stageCreated []appmodel.CRMStageCreateRequest
}

func (f *fakeStore) UpdateCRMDeal(_ context.Context, request appmodel.CRMDealUpdateRequest) (model.CRMDeal, error) {
	f.updated = append(f.updated, request)
	return model.CRMDeal{ID: request.DealID}, nil
}

func (f *fakeStore) ListCRMStages(context.Context, appmodel.CRMStageListQuery) ([]model.CRMStage, error) {
	return f.stages, nil
}

func (f *fakeStore) CreateCRMStage(_ context.Context, request appmodel.CRMStageCreateRequest) (model.CRMStage, error) {
	f.stageCreated = append(f.stageCreated, request)
	return model.CRMStage{Name: request.Name, Kind: request.Kind}, nil
}

func (f *fakeStore) CreateCRMDeal(_ context.Context, request appmodel.CRMDealCreateRequest) (model.CRMDeal, error) {
	f.created = append(f.created, request)
	return model.CRMDeal{Title: request.Title, Stage: request.Stage}, nil
}

func (f *fakeStore) ListCRMDeals(context.Context, appmodel.CRMDealListQuery) ([]model.CRMDeal, error) {
	return f.deals, nil
}

func newTestService(t *testing.T, store Store) *Service {
	t.Helper()
	return newServiceWithProjects(t, store, &fakeProjects{})
}

func newServiceWithProjects(t *testing.T, store Store, projects ProjectWorkflow) *Service {
	t.Helper()
	svc, err := New(Dependencies{Store: store, Projects: projects})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

// fakeProjects records the project requests and serves the project it makes.
type fakeProjects struct {
	createErr error
	created   []appmodel.ProjectCreateRequest
	deleted   []appmodel.ProjectMutationRequest
}

func (f *fakeProjects) Create(_ context.Context, request appmodel.ProjectCreateRequest) (model.Project, error) {
	f.created = append(f.created, request)
	if f.createErr != nil {
		return model.Project{}, f.createErr
	}
	return model.Project{ID: 77, Name: request.Name}, nil
}

func (f *fakeProjects) Delete(_ context.Context, request appmodel.ProjectMutationRequest) error {
	f.deleted = append(f.deleted, request)
	return nil
}

// dealStore serves one deal to the project step and records the link it is asked to write.
type dealStore struct {
	fakeStore
	deal    model.CRMDeal
	linkErr error
	linked  []appmodel.CRMDealProjectLinkRequest
}

func (f *dealStore) CRMDealForProject(context.Context, appmodel.CRMDealProjectRequest) (model.CRMDeal, error) {
	return f.deal, nil
}

func (f *dealStore) LinkCRMDealProject(_ context.Context, request appmodel.CRMDealProjectLinkRequest) (model.CRMDeal, error) {
	f.linked = append(f.linked, request)
	if f.linkErr != nil {
		return model.CRMDeal{}, f.linkErr
	}
	return f.deal, nil
}

func wonDeal() model.CRMDeal {
	return model.CRMDeal{ID: 5, TeamID: 1, Title: "Лендинг", Stage: model.DealStageWon, StageKind: model.StageKindWon, ClientID: 9, AmountCents: 250000, Currency: "RUB"}
}

func wantField(t *testing.T, err error, field string) {
	t.Helper()
	var fe appmodel.FieldError
	if !errors.As(err, &fe) || fe.Field != field {
		t.Fatalf("error = %v, want a field error on %q", err, field)
	}
}

func TestCreateDealTrimsDefaultsAndRefusesBadInput(t *testing.T) {
	store := &fakeStore{}
	svc := newTestService(t, store)
	ctx := context.Background()

	if _, err := svc.CreateDeal(ctx, appmodel.CRMDealCreateRequest{TeamID: 1, Title: "   "}); err == nil {
		t.Fatal("blank title was accepted")
	} else {
		wantField(t, err, "deal title")
	}
	if _, err := svc.CreateDeal(ctx, appmodel.CRMDealCreateRequest{TeamID: 1, Title: "Сайт", Probability: 101}); err == nil {
		t.Fatal("probability above 100 was accepted")
	} else {
		wantField(t, err, "probability")
	}
	if _, err := svc.CreateDeal(ctx, appmodel.CRMDealCreateRequest{TeamID: 1, Title: "Сайт", CloseDate: "2026-13-01"}); err == nil {
		t.Fatal("impossible close date was accepted")
	} else {
		wantField(t, err, "close date")
	}
	if _, err := svc.CreateDeal(ctx, appmodel.CRMDealCreateRequest{TeamID: 1, Title: "Сайт", Currency: "rubles", Manager: true}); err == nil {
		t.Fatal("long currency was accepted")
	} else {
		wantField(t, err, "currency")
	}
	if len(store.created) != 0 {
		t.Fatalf("a refused deal reached the store: %+v", store.created)
	}

	deal, err := svc.CreateDeal(ctx, appmodel.CRMDealCreateRequest{TeamID: 1, Title: "  Сайт  ", Currency: "rub", NextStepAt: "2026-10-30", Manager: true})
	if err != nil {
		t.Fatal(err)
	}
	if deal.Title != "Сайт" || len(store.created) != 1 {
		t.Fatalf("created = %+v, deal = %+v", store.created, deal)
	}
	if got := store.created[0]; got.Stage != "" || got.Currency != "RUB" {
		t.Fatalf("stage and currency = %q, %q; want no stage (the workspace picks its first open one) and RUB", got.Stage, got.Currency)
	}
}

// A member cannot set a deal's amount or currency. A create or an update that names one is
// refused on "amount". A member's zero is no amount given, so the update leaves the stored
// amount, which a manager may have set, as it is.
func TestMemberCannotSetADealAmount(t *testing.T) {
	store := &fakeStore{}
	svc := newTestService(t, store)
	ctx := context.Background()

	_, err := svc.CreateDeal(ctx, appmodel.CRMDealCreateRequest{TeamID: 1, Title: "Сайт", AmountCents: 250000})
	wantField(t, err, "amount")
	_, err = svc.CreateDeal(ctx, appmodel.CRMDealCreateRequest{TeamID: 1, Title: "Сайт", Currency: "RUB"})
	wantField(t, err, "amount")
	if _, err := svc.CreateDeal(ctx, appmodel.CRMDealCreateRequest{TeamID: 1, Title: "Сайт"}); err != nil {
		t.Fatalf("a member's deal without an amount: %v", err)
	}
	if len(store.created) != 1 {
		t.Fatalf("created = %+v, want only the deal without an amount", store.created)
	}

	amount, zero, title := int64(250000), int64(0), "Лендинг"
	_, err = svc.UpdateDeal(ctx, appmodel.CRMDealUpdateRequest{TeamID: 1, DealID: 5, AmountCents: &amount})
	wantField(t, err, "amount")
	if _, err := svc.UpdateDeal(ctx, appmodel.CRMDealUpdateRequest{TeamID: 1, DealID: 5, Title: &title, AmountCents: &zero}); err != nil {
		t.Fatalf("a member's title change with a zero amount: %v", err)
	}
	if len(store.updated) != 1 || store.updated[0].AmountCents != nil || store.updated[0].Title == nil {
		t.Fatalf("updated = %+v, want the title alone", store.updated)
	}
	if _, err := svc.UpdateDeal(ctx, appmodel.CRMDealUpdateRequest{TeamID: 1, DealID: 5, Manager: true, AmountCents: &amount}); err != nil {
		t.Fatalf("a manager's amount: %v", err)
	}
	if got := store.updated[1]; got.AmountCents == nil || *got.AmountCents != amount {
		t.Fatalf("a manager's amount did not reach the store: %+v", got)
	}
}

func TestDealsOverdueNeedsTheWorkspaceDay(t *testing.T) {
	svc := newTestService(t, &fakeStore{})
	_, err := svc.Deals(context.Background(), appmodel.CRMDealListQuery{TeamID: 1, Overdue: true})
	wantField(t, err, "today")
}

func TestPipelineCountsEveryStageInItsOrderAndTheOverdueOpenDeals(t *testing.T) {
	stages := []model.CRMStage{
		{Key: "new", Name: "Новая", Kind: model.StageKindOpen},
		{Key: "chat", Name: "Чат", Kind: model.StageKindOpen},
		{Key: "won", Name: "Выиграна", Kind: model.StageKindWon},
		{Key: "lost", Name: "Проиграна", Kind: model.StageKindLost},
	}
	store := &fakeStore{stages: stages, deals: []model.CRMDeal{
		{Stage: "new", StageKind: model.StageKindOpen, NextStepAt: "2026-10-01"},
		{Stage: "new", StageKind: model.StageKindOpen, NextStepAt: "2026-10-30"},
		{Stage: "won", StageKind: model.StageKindWon, NextStepAt: "2026-10-01"},
		{Stage: "lost", StageKind: model.StageKindLost},
	}}
	svc := newTestService(t, store)
	pipeline, err := svc.Pipeline(context.Background(), appmodel.CRMDealListQuery{TeamID: 1, Today: "2026-10-24"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pipeline.Stages) != len(stages) {
		t.Fatalf("stages = %+v, want the workspace's own", pipeline.Stages)
	}
	if pipeline.Stages[1].Stage != "chat" || pipeline.Stages[1].Name != "Чат" || pipeline.Stages[1].Kind != model.StageKindOpen {
		t.Fatalf("stage order and names = %+v", pipeline.Stages)
	}
	counts := map[string]int{}
	for _, stage := range pipeline.Stages {
		counts[stage.Stage] = stage.Count
	}
	if counts["new"] != 2 || counts["chat"] != 0 || counts["won"] != 1 || counts["lost"] != 1 {
		t.Fatalf("counts = %+v", counts)
	}
	if pipeline.Overdue != 1 {
		t.Fatalf("overdue = %d, want only the open deal with a past next step", pipeline.Overdue)
	}
}

// Only a manager changes the pipeline, and a stage needs a name and a kind of the three.
func TestStageChangesRefuseMembersAndBadInput(t *testing.T) {
	store := &fakeStore{}
	svc := newTestService(t, store)
	ctx := context.Background()

	if _, err := svc.CreateStage(ctx, appmodel.CRMStageCreateRequest{TeamID: 1, Name: "Этап"}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("a member created a stage = %v, want ErrForbidden", err)
	}
	if len(store.stageCreated) != 0 {
		t.Fatal("a refused stage reached the store")
	}
	if _, err := svc.CreateStage(ctx, appmodel.CRMStageCreateRequest{TeamID: 1, Manager: true, Name: "  "}); err == nil {
		t.Fatal("blank stage name was accepted")
	} else {
		wantField(t, err, "stage name")
	}
	if _, err := svc.CreateStage(ctx, appmodel.CRMStageCreateRequest{TeamID: 1, Manager: true, Name: strings.Repeat("a", 201)}); err == nil {
		t.Fatal("stage name above 200 bytes was accepted")
	} else {
		wantField(t, err, "stage name")
	}
	if _, err := svc.CreateStage(ctx, appmodel.CRMStageCreateRequest{TeamID: 1, Manager: true, Name: "Этап", Kind: "closed"}); err == nil {
		t.Fatal("unknown stage kind was accepted")
	} else {
		wantField(t, err, "kind")
	}

	created, err := svc.CreateStage(ctx, appmodel.CRMStageCreateRequest{TeamID: 1, Manager: true, Name: "  Демо  "})
	if err != nil {
		t.Fatal(err)
	}
	if got := store.stageCreated[0]; got.Name != "Демо" || got.Kind != model.StageKindOpen || created.Kind != model.StageKindOpen {
		t.Fatalf("created = %+v, request = %+v; want the trimmed name and an open kind by default", created, got)
	}

	empty, kind, zero := "", "closed", 0
	if _, err := svc.UpdateStage(ctx, appmodel.CRMStageUpdateRequest{TeamID: 1, Manager: true, Key: "new", Name: &empty}); err == nil {
		t.Fatal("renaming a stage to nothing was accepted")
	} else {
		wantField(t, err, "stage name")
	}
	if _, err := svc.UpdateStage(ctx, appmodel.CRMStageUpdateRequest{TeamID: 1, Manager: true, Key: "new", Kind: &kind}); err == nil {
		t.Fatal("unknown kind on update was accepted")
	} else {
		wantField(t, err, "kind")
	}
	if _, err := svc.MoveStage(ctx, appmodel.CRMStageMoveRequest{TeamID: 1, Manager: true, Key: "new", Position: zero}); err == nil {
		t.Fatal("position 0 was accepted")
	} else {
		wantField(t, err, "position")
	}
	if err := svc.DeleteStage(ctx, appmodel.CRMStageDeleteRequest{TeamID: 1, Key: "new"}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("a member deleted a stage = %v, want ErrForbidden", err)
	}
}

func TestLogActivityDefaultsToANoteAndNeedsAnAnchor(t *testing.T) {
	svc := newTestService(t, &fakeStore{})
	ctx := context.Background()
	if _, err := svc.LogActivity(ctx, appmodel.CRMActivityCreateRequest{TeamID: 1, Body: "созвон"}); err == nil {
		t.Fatal("an activity without a client or deal was accepted")
	} else {
		wantField(t, err, "client or deal")
	}
	if _, err := svc.LogActivity(ctx, appmodel.CRMActivityCreateRequest{TeamID: 1, ClientID: 3}); err == nil {
		t.Fatal("an empty activity was accepted")
	} else {
		wantField(t, err, "body")
	}
}

func TestNewNeedsAStore(t *testing.T) {
	if _, err := New(Dependencies{}); !errors.Is(err, ErrIncompleteDependencies) {
		t.Fatalf("New without a store = %v", err)
	}
	if _, err := New(Dependencies{Store: &fakeStore{}}); !errors.Is(err, ErrIncompleteDependencies) {
		t.Fatalf("New without a projects workflow = %v", err)
	}
}

// A won deal makes a project for its client with its amount as the budget and no rate, and
// the deal keeps the project. A deal that is not won, already has one or has no client is
// refused before the projects workflow is asked.
func TestCreateDealProjectMakesTheProjectForTheDeal(t *testing.T) {
	ctx := t.Context()
	store := &dealStore{deal: wonDeal()}
	projects := &fakeProjects{}
	svc := newServiceWithProjects(t, store, projects)

	if _, err := svc.CreateDealProject(ctx, appmodel.CRMDealProjectRequest{TeamID: 1, CallerID: 2, DealID: 5}); err != nil {
		t.Fatalf("create project of a won deal: %v", err)
	}
	got := projects.created[0]
	if got.Name != "Лендинг" || got.ClientID != 9 || got.BudgetCents == nil || *got.BudgetCents != 250000 || got.RateCents != nil || got.Currency != "RUB" {
		t.Fatalf("project request = %+v", got)
	}
	if len(store.linked) != 1 || store.linked[0].ProjectID != 77 || store.linked[0].DealID != 5 {
		t.Fatalf("link = %+v, want deal 5 linked to project 77", store.linked)
	}

	refusals := map[string]func(*model.CRMDeal){
		"stage":   func(d *model.CRMDeal) { d.StageKind = model.StageKindOpen },
		"project": func(d *model.CRMDeal) { d.ProjectID = 3 },
		"client":  func(d *model.CRMDeal) { d.ClientID = 0 },
	}
	for field, change := range refusals {
		deal := wonDeal()
		change(&deal)
		store := &dealStore{deal: deal}
		projects := &fakeProjects{}
		_, err := newServiceWithProjects(t, store, projects).CreateDealProject(ctx, appmodel.CRMDealProjectRequest{TeamID: 1, CallerID: 2, DealID: 5})
		wantField(t, err, field)
		if len(projects.created) != 0 || len(store.linked) != 0 {
			t.Fatalf("%s refusal still reached the projects workflow: created %d, linked %d", field, len(projects.created), len(store.linked))
		}
	}
}

// The projects workflow owns the manager rule: its refusal of a member comes back as the
// reason, and a name another project has is a field the person can change.
func TestCreateDealProjectNamesWhyTheProjectWasRefused(t *testing.T) {
	store := &dealStore{deal: wonDeal()}
	svc := newServiceWithProjects(t, store, &fakeProjects{createErr: model.ErrForbidden})
	if _, err := svc.CreateDealProject(t.Context(), appmodel.CRMDealProjectRequest{TeamID: 1, CallerID: 2, DealID: 5}); !errors.Is(err, model.ErrProjectManagerRequired) {
		t.Fatalf("member refused by the projects rule = %v, want ErrProjectManagerRequired", err)
	}

	svc = newServiceWithProjects(t, store, &fakeProjects{createErr: model.ErrAlreadyExists})
	_, err := svc.CreateDealProject(t.Context(), appmodel.CRMDealProjectRequest{TeamID: 1, CallerID: 2, DealID: 5})
	wantField(t, err, "project name")
	if len(store.linked) != 0 {
		t.Fatal("a refused project was linked to the deal")
	}
}

// When the link is refused because the deal changed while its project was made, the project
// goes too, so no project is left behind for a deal that does not hold it.
func TestCreateDealProjectRemovesTheProjectWhenTheLinkIsRefused(t *testing.T) {
	store := &dealStore{deal: wonDeal(), linkErr: appmodel.FieldError{Field: "stage"}}
	projects := &fakeProjects{}
	_, err := newServiceWithProjects(t, store, projects).CreateDealProject(t.Context(), appmodel.CRMDealProjectRequest{TeamID: 1, CallerID: 2, DealID: 5})
	wantField(t, err, "stage")
	if len(projects.deleted) != 1 || projects.deleted[0].ProjectID != 77 || projects.deleted[0].CallerID != 2 {
		t.Fatalf("removed projects = %+v, want project 77", projects.deleted)
	}
}
