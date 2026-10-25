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
	deals        []model.CRMDeal
	stages       []model.CRMStage
	stageCreated []appmodel.CRMStageCreateRequest
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
	svc, err := New(Dependencies{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	return svc
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
	if _, err := svc.CreateDeal(ctx, appmodel.CRMDealCreateRequest{TeamID: 1, Title: "Сайт", Currency: "rubles"}); err == nil {
		t.Fatal("long currency was accepted")
	} else {
		wantField(t, err, "currency")
	}
	if len(store.created) != 0 {
		t.Fatalf("a refused deal reached the store: %+v", store.created)
	}

	deal, err := svc.CreateDeal(ctx, appmodel.CRMDealCreateRequest{TeamID: 1, Title: "  Сайт  ", Currency: "rub", NextStepAt: "2026-10-30"})
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
}
